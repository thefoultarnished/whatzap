package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/rivo/uniseg"
	qrcode "github.com/skip2/go-qrcode"
)

type mediaSendCommand struct {
	kind    string
	path    string
	caption string
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if n <= 3 || len(r) <= n {
		return string(r)
	}
	return string(r[:n-3]) + "..."
}

func saveConfig() {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".whatzap_tui_config.json")
	data, _ := json.Marshal(currentConfig)
	_ = os.WriteFile(path, data, 0644)
}

func loadConfig() {
	currentConfig = Config{
		ThemeName:    "tokyonight",
		MouseEnabled: true,
	}
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".whatzap_tui_config.json")
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &currentConfig)
	}
}

func sanitizeOutgoingText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.IsPrint(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func hasVisibleText(s string) bool {
	return strings.TrimSpace(sanitizeOutgoingText(s)) != ""
}

func graphemeCount(s string) int {
	g := uniseg.NewGraphemes(s)
	n := 0
	for g.Next() {
		n++
	}
	return n
}

func graphemeDeleteLast(s string) string {
	g := uniseg.NewGraphemes(s)
	var clusters []string
	for g.Next() {
		clusters = append(clusters, g.Str())
	}
	if len(clusters) == 0 {
		return s
	}
	return strings.Join(clusters[:len(clusters)-1], "")
}

func graphemeSliceN(s string, n int) string {
	g := uniseg.NewGraphemes(s)
	var b strings.Builder
	i := 0
	for g.Next() && i < n {
		b.WriteString(g.Str())
		i++
	}
	return b.String()
}

func graphemeSplitFirst(s string) (first, rest string) {
	g := uniseg.NewGraphemes(s)
	if !g.Next() {
		return "", ""
	}
	first = g.Str()
	var b strings.Builder
	for g.Next() {
		b.WriteString(g.Str())
	}
	return first, b.String()
}

func countEmojiHeuristic(s string) int {
	count := 0
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size <= 1 {
			break
		}
		if (r >= 0x1F300 && r <= 0x1F5FF) ||
			(r >= 0x1F600 && r <= 0x1F64F) ||
			(r >= 0x1F680 && r <= 0x1F6FF) ||
			(r >= 0x1F700 && r <= 0x1F77F) ||
			(r >= 0x1F780 && r <= 0x1F7FF) ||
			(r >= 0x1F900 && r <= 0x1F9FF) ||
			(r >= 0x1FA70 && r <= 0x1FAFF) ||
			(r >= 0x2600 && r <= 0x26FF) ||
			(r >= 0x2700 && r <= 0x27BF) {
			count++
		}
		s = s[size:]
	}
	return count
}

func runeDisplayWidth(s string) int {
	extra := 0
	for _, r := range s {
		if r >= 0x1F300 {
			extra++
		}
	}
	return len([]rune(s)) + extra
}

func wrapText(s string, width int) string {
	if width < 4 {
		return s
	}
	chunkWord := func(w string) []string {
		r := []rune(w)
		var out []string
		start := 0
		cur := 0
		for i := range r {
			cw := runeDisplayWidth(string(r[i : i+1]))
			if cur+cw > width && i > start {
				out = append(out, string(r[start:i]))
				start = i
				cur = 0
			}
			cur += cw
		}
		if start < len(r) {
			out = append(out, string(r[start:]))
		}
		return out
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return ""
	}
	lines := make([]string, 0, 4)
	cur := ""
	for _, w := range words {
		chunks := chunkWord(w)
		for _, chunk := range chunks {
			if cur == "" {
				cur = chunk
				continue
			}
			if runeDisplayWidth(cur)+1+runeDisplayWidth(chunk) <= width {
				cur += " " + chunk
			} else {
				lines = append(lines, cur)
				cur = chunk
			}
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

func renderQR(payload string, maxW, maxH int) string {
	p := strings.TrimSpace(payload)
	if p == "" {
		return ""
	}
	qr, err := qrcode.New(p, qrcode.Low)
	if err != nil {
		return ""
	}
	bitmap := qr.Bitmap()
	if len(bitmap) == 0 || len(bitmap[0]) == 0 {
		return ""
	}
	rows := len(bitmap)
	cols := len(bitmap[0])
	scale := 1
	if maxW > 0 && cols > maxW {
		scale = (cols + maxW - 1) / maxW
	}
	if maxH > 0 {
		vScale := (rows + 2*maxH - 1) / (2 * maxH)
		if vScale > scale {
			scale = vScale
		}
	}
	lines := make([]string, 0, rows/(2*scale)+1)
	stepY := 2 * scale
	for y := 0; y < rows; y += stepY {
		var line strings.Builder
		for x := 0; x < cols; x += scale {
			top := bitmap[y][x]
			bot := y+scale < rows && bitmap[y+scale][x]
			switch {
			case top && bot:
				line.WriteString(bb)
			case top && !bot:
				line.WriteString(bw)
			case !top && bot:
				line.WriteString(wb)
			default:
				line.WriteString(ww)
			}
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func formatRelativeTime(ts int64) string {
	if ts == 0 {
		return ""
	}
	t := time.Unix(ts, 0)
	diff := time.Since(t)
	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	}
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("03:04 PM")
	}
	y := now.AddDate(0, 0, -1)
	if t.Year() == y.Year() && t.YearDay() == y.YearDay() {
		return "Yesterday"
	}
	if diff < 7*24*time.Hour {
		return t.Format("Mon")
	}
	return t.Format("Jan 2")
}

func dateSeparatorLabel(ts int64) string {
	if ts == 0 {
		return ""
	}
	t := time.Unix(ts, 0)
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return "Today"
	}
	yesterday := now.AddDate(0, 0, -1)
	if t.Year() == yesterday.Year() && t.YearDay() == yesterday.YearDay() {
		return "Yesterday"
	}
	return t.Format("02 Jan 2006")
}

func formatExactTime(ts int64) string {
	if ts == 0 {
		return ""
	}
	t := time.Unix(ts, 0)
	diff := time.Since(t)
	if diff < 0 {
		diff = 0
	}
	if diff < time.Minute {
		return "just now"
	}
	if diff < time.Hour {
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	}
	return t.Format("03:04 PM")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func num(jid string) string {
	p := strings.Split(jid, "@")
	return p[0]
}

func renderMessageBody(m map[string]any) string {
	if v, ok := m["conversation"].(string); ok {
		return v
	}
	if v, ok := m["extendedTextMessage"].(map[string]any); ok {
		if t, ok2 := v["text"].(string); ok2 {
			return t
		}
	}
	if v, ok := m["imageMessage"].(map[string]any); ok {
		return renderMediaSummary(imageTagStyle.Render("[image]"), v)
	}
	if v, ok := m["videoMessage"].(map[string]any); ok {
		return renderMediaSummary(videoTagStyle.Render("[video]"), v)
	}
	if v, ok := m["documentMessage"].(map[string]any); ok {
		return renderMediaSummary(fileTagStyle.Render("[file]"), v)
	}
	if v, ok := m["audioMessage"].(map[string]any); ok {
		if ptt, _ := v["ptt"].(bool); ptt {
			return audioTagStyle.Render("[voice]")
		}
		return audioTagStyle.Render("[audio]")
	}
	if _, ok := m["stickerMessage"]; ok {
		return stickerTagStyle.Render("[sticker]")
	}
	if v, ok := m["reactionMessage"].(map[string]any); ok {
		if emoji, _ := v["emoji"].(string); emoji != "" {
			return emoji
		}
		return "[reaction]"
	}
	if v, ok := m["unknown"].(map[string]any); ok {
		if label := unknownMessageLabel(v); label != "" {
			return renderUnknownTag(label)
		}
		return anomalyTagStyle.Render("[anomaly]")
	}
	return ""
}

func renderMediaSummary(tag string, payload map[string]any) string {
	name, _ := payload["fileName"].(string)
	caption, _ := payload["caption"].(string)

	parts := []string{tag}
	if name != "" {
		parts = append(parts, "["+name+"]")
	}
	if caption != "" {
		parts = append(parts, caption)
	}
	return strings.Join(parts, " ")
}

func renderUnknownTag(label string) string {
	switch label {
	case "image":
		return imageTagStyle.Render("[image]")
	case "video":
		return videoTagStyle.Render("[video]")
	case "audio":
		return audioTagStyle.Render("[audio]")
	case "voice":
		return audioTagStyle.Render("[voice]")
	case "file":
		return fileTagStyle.Render("[file]")
	case "sticker":
		return stickerTagStyle.Render("[sticker]")
	case "contact":
		return contactTagStyle.Render("[contact]")
	case "poll":
		return pollTagStyle.Render("[poll]")
	case "location":
		return locationTagStyle.Render("[location]")
	case "anomaly":
		return anomalyTagStyle.Render("[anomaly]")
	default:
		if label == "" {
			return anomalyTagStyle.Render("[anomaly]")
		}
		return anomalyTagStyle.Render("[" + label + "]")
	}
}

func unknownMessageLabel(v map[string]any) string {
	for _, key := range []string{"effectiveFields", "rawFields"} {
		if label := firstUnknownField(v[key]); label != "" {
			return prettyUnknownLabel(label)
		}
	}
	return ""
}

func prettyUnknownLabel(label string) string {
	switch label {
	case "imageMessage", "eventCoverImage", "pollCreationOptionImageMessage":
		return "image"
	case "videoMessage", "ptvMessage":
		return "video"
	case "audioMessage":
		return "audio"
	case "documentMessage", "documentWithCaptionMessage":
		return "file"
	case "stickerMessage", "lottieStickerMessage", "stickerPackMessage", "statusStickerInteractionMessage":
		return "sticker"
	case "contactMessage":
		return "contact"
	case "pollCreationMessage", "pollCreationMessageV2", "pollCreationMessageV3", "pollCreationMessageV4", "pollCreationMessageV5":
		return "poll"
	case "locationMessage", "liveLocationMessage":
		return "location"
	default:
		return label
	}
}

func firstUnknownField(v any) string {
	switch vals := v.(type) {
	case []string:
		for _, s := range vals {
			if strings.TrimSpace(s) != "" {
				return s
			}
		}
	case []any:
		for _, item := range vals {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

func isMediaWire(msg wireMsg) bool {
	if msg.MediaProto != "" {
		return true
	}
	m := msg.Message
	if m == nil {
		return false
	}
	if _, ok := m["conversation"]; ok && len(m) == 1 {
		return false
	}
	if _, ok := m["extendedTextMessage"]; ok && len(m) == 1 {
		return false
	}
	if _, ok := m["imageMessage"]; ok {
		return true
	}
	if _, ok := m["videoMessage"]; ok {
		return true
	}
	if _, ok := m["documentMessage"]; ok {
		return true
	}
	if _, ok := m["audioMessage"]; ok {
		return true
	}
	if _, ok := m["stickerMessage"]; ok {
		return true
	}
	if _, ok := m["documentWithCaptionMessage"]; ok {
		return true
	}
	if _, ok := m["viewOnceMessage"]; ok {
		return true
	}
	if _, ok := m["viewOnceMessageV2"]; ok {
		return true
	}
	if _, ok := m["viewOnceMessageV2Extension"]; ok {
		return true
	}
	if _, ok := m["ephemeralMessage"]; ok {
		return true
	}
	if _, ok := m["locationMessage"]; ok {
		return true
	}
	if _, ok := m["liveLocationMessage"]; ok {
		return true
	}
	if _, ok := m["contactMessage"]; ok {
		return true
	}
	if _, ok := m["contactsArrayMessage"]; ok {
		return true
	}
	if _, ok := m["pollCreationMessage"]; ok {
		return true
	}
	if _, ok := m["ptvMessage"]; ok {
		return true
	}
	for k := range m {
		if k != "conversation" && k != "extendedTextMessage" && k != "reactionMessage" {
			return true
		}
	}
	return false
}

func commandBestMatch(input string) string {
	in := strings.ToLower(strings.TrimSpace(input))
	if !strings.HasPrefix(in, "/") {
		return ""
	}
	if theme := themeCommandBestMatch(in); theme != "" {
		return theme
	}
	for _, c := range systemCommands {
		if strings.HasPrefix(c, in) && c != in {
			return c
		}
	}
	return ""
}

func chatCommandBestMatch(input string) string {
	in := strings.ToLower(strings.TrimSpace(input))
	if !strings.HasPrefix(in, "/") {
		return ""
	}
	for _, c := range chatCommands {
		if strings.HasPrefix(c, in) && c != in {
			return c
		}
	}
	return ""
}

func isMediaSendCommand(cmd string) bool {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "/send", "/sendimage", "/sendvideo", "/sendfile":
		return true
	default:
		return false
	}
}

func mediaSendCommandPrefix(input string) (string, string, bool) {
	in := strings.TrimSpace(input)
	for _, cmd := range chatCommands {
		if in == cmd {
			return cmd, "", true
		}
		if strings.HasPrefix(in, cmd+" ") {
			return cmd, strings.TrimSpace(strings.TrimPrefix(in, cmd)), true
		}
	}
	for _, cmd := range []string{"/sendimage", "/sendvideo", "/sendfile"} {
		if in == cmd {
			return cmd, "", true
		}
		if strings.HasPrefix(in, cmd+" ") {
			return cmd, strings.TrimSpace(strings.TrimPrefix(in, cmd)), true
		}
	}
	return "", "", false
}

func completeChatInputStep(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return "", false
	}

	if best := chatCommandBestMatch(trimmed); best != "" {
		if isMediaSendCommand(best) {
			return best + " ", true
		}
		return best, true
	}

	cmd, rest, ok := mediaSendCommandPrefix(trimmed)
	if !ok {
		return "", false
	}
	if rest == "" && !strings.HasSuffix(input, " ") {
		return cmd + " ", true
	}
	if rest == "" {
		return "", false
	}

	_, caption, parsed := splitMediaPathAndCaption(rest)
	if parsed && caption == "" && !strings.HasSuffix(input, " ") {
		return input + " ", true
	}
	return "", false
}

func chatInputGhost(input string) string {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return ""
	}

	if best := chatCommandBestMatch(trimmed); best != "" && strings.HasPrefix(best, strings.ToLower(trimmed)) {
		return best[len(trimmed):]
	}

	_, rest, ok := mediaSendCommandPrefix(trimmed)
	if !ok {
		return ""
	}
	if rest == "" {
		if strings.HasSuffix(input, " ") {
			return "<file path>"
		}
		return " <file path>"
	}

	_, caption, parsed := splitMediaPathAndCaption(rest)
	if parsed && caption == "" {
		if strings.HasSuffix(input, " ") {
			return "[caption]"
		}
		return " [caption]"
	}
	return ""
}

func themeCommandBestMatch(in string) string {
	switch {
	case strings.HasPrefix("/theme", in) && in != "/theme":
		return "/theme"
	case in == "/theme":
		return "/theme1tokyonight"
	case strings.HasPrefix(in, "/theme1") && in != "/theme1tokyonight":
		return "/theme1tokyonight"
	case strings.HasPrefix(in, "/theme2") && in != "/theme2catppuccin":
		return "/theme2catppuccin"
	case strings.HasPrefix(in, "/theme3") && in != "/theme3monokai":
		return "/theme3monokai"
	case strings.HasPrefix(in, "/theme4") && in != "/theme4charcoal":
		return "/theme4charcoal"
	}
	return ""
}

func parseMediaSendCommand(input string) (mediaSendCommand, string, bool) {
	type spec struct {
		cmd   string
		kind  string
		usage string
	}
	specs := []spec{
		{cmd: "/send", kind: "", usage: "usage: /send <path> [caption]"},
		// Old send aliases.
		{cmd: "/sendimage", kind: "image", usage: "usage: /sendimage <path> [caption]"},
		{cmd: "/sendvideo", kind: "video", usage: "usage: /sendvideo <path> [caption]"},
		{cmd: "/sendfile", kind: "document", usage: "usage: /sendfile <path> [caption]"},
	}
	in := strings.TrimSpace(input)
	for _, s := range specs {
		if in != s.cmd && !strings.HasPrefix(in, s.cmd+" ") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(in, s.cmd))
		if rest == "" {
			return mediaSendCommand{}, s.usage, true
		}
		path, caption, ok := splitMediaPathAndCaption(rest)
		if !ok {
			return mediaSendCommand{}, s.usage + `; quote paths with spaces if needed`, true
		}
		return mediaSendCommand{kind: s.kind, path: path, caption: caption}, "", true
	}
	return mediaSendCommand{}, "", false
}

func splitMediaPathAndCaption(s string) (string, string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	if s[0] == '"' || s[0] == '\'' {
		quote := s[0]
		end := strings.IndexByte(s[1:], quote)
		if end < 0 {
			return "", "", false
		}
		path := s[1 : 1+end]
		caption := strings.TrimSpace(s[2+end:])
		if path == "" {
			return "", "", false
		}
		return path, caption, true
	}
	if stat, err := os.Stat(s); err == nil && !stat.IsDir() {
		return s, "", true
	}
	parts := strings.Fields(s)
	for i := len(parts); i >= 1; i-- {
		path := strings.Join(parts[:i], " ")
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path, strings.Join(parts[i:], " "), true
		}
	}
	return "", "", false
}

func detectMediaSendKind(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file not found")
	}
	if info.IsDir() {
		return "", fmt.Errorf("path must be a file")
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return "image", nil
	case ".mp4", ".mov", ".avi", ".mkv", ".webm", ".3gp", ".m4v":
		return "video", nil
	}

	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file")
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read file")
	}
	mimeType := http.DetectContentType(buf[:n])
	if strings.HasPrefix(mimeType, "image/") {
		return "image", nil
	}
	if strings.HasPrefix(mimeType, "video/") {
		return "video", nil
	}
	return "document", nil
}

func parseAtReplyToken(s string) (string, int, bool) {
	if !strings.HasPrefix(s, "@") || len(s) < 3 {
		return "", 0, false
	}
	cat := unicode.ToUpper(rune(s[1]))
	if cat != 'R' && cat != 'S' && cat != 'M' {
		return "", 0, false
	}
	i := 2
	startDigits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == startDigits {
		return "", 0, false
	}
	n, err := strconv.Atoi(s[startDigits:i])
	if err != nil || n < 1 {
		return "", 0, false
	}
	end := i
	if i < len(s) && s[i] == ' ' {
		end = i + 1
	}
	return fmt.Sprintf("%c%d", cat, n), end, true
}

func atReplyPrefixEnd(s string) int {
	_, end, ok := parseAtReplyToken(s)
	if !ok {
		return 0
	}
	return end
}

func dateSeparatorLine(label string, w int) string {
	if label == "" {
		return ""
	}
	styled := dateSepStyle.Render(label)
	return lipgloss.NewStyle().Width(w - 2).Align(lipgloss.Center).Render(styled)
}
