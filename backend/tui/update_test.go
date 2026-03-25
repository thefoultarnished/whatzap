package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestChatInputLockedIgnoresTyping(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{},
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	got := next.(m)

	if got.input != "" {
		t.Fatalf("input = %q, want empty", got.input)
	}
	if got.inputBuf != "" {
		t.Fatalf("inputBuf = %q, want empty", got.inputBuf)
	}
}

func TestRenderChatInputShowsBlacklistedPlaceholder(t *testing.T) {
	currentTheme = TokyoNight
	rehashStyles()

	model := m{
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{},
	}

	rendered := model.renderChatInput(48, "")
	if !strings.Contains(rendered, "blacklisted") {
		t.Fatalf("rendered input missing blacklisted placeholder: %q", rendered)
	}
	if !strings.Contains(rendered, "/whitelist") {
		t.Fatalf("rendered input missing whitelist hint: %q", rendered)
	}
}

func TestRenderChatInputFocusedTypableShowsBlinkingCursorState(t *testing.T) {
	currentTheme = TokyoNight
	rehashStyles()

	base := m{
		mode:             "chat",
		active:           "15551230001@s.whatsapp.net",
		whitelist:        map[string]string{"15551230001": "Allowed"},
		sidebarFocused:   false,
		leftInputFocused: false,
	}

	onModel := base
	onModel.cursorOn = true
	offModel := base
	offModel.cursorOn = false

	renderedOn := onModel.renderChatInput(48, "")
	renderedOff := offModel.renderChatInput(48, "")

	if renderedOn == renderedOff {
		t.Fatalf("expected locked focused input to render different blink states")
	}
}

func TestAltFOpensFilePickerInChat(t *testing.T) {
	currentTheme = TokyoNight
	rehashStyles()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(filePath, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	model := m{
		status:         "ready",
		mode:           "chat",
		active:         "15551230001@s.whatsapp.net",
		whitelist:      map[string]string{"15551230001": "Allowed"},
		fileBrowserDir: dir,
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f"), Alt: true})
	got := next.(m)

	if !got.fileBrowserOpen {
		t.Fatal("expected file browser to open")
	}
	if got.fileBrowserDir != dir {
		t.Fatalf("fileBrowserDir = %q, want %q", got.fileBrowserDir, dir)
	}
	if len(got.fileBrowserEntries) == 0 {
		t.Fatal("expected file browser entries")
	}
}

func TestFilePickerSelectFileInsertsSendCommand(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "report.pdf")
	if err := os.WriteFile(filePath, []byte("pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	model := m{
		status:         "ready",
		mode:           "chat",
		active:         "15551230001@s.whatsapp.net",
		whitelist:      map[string]string{"15551230001": "Allowed"},
		fileBrowserDir: dir,
	}
	if err := model.loadFileBrowserDir(dir); err != nil {
		t.Fatal(err)
	}
	model.fileBrowserOpen = true
	for i, entry := range model.fileBrowserEntries {
		if entry.path == filePath {
			model.fileBrowserIndex = i
			break
		}
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(m)

	if got.fileBrowserOpen {
		t.Fatal("expected file browser to close after selecting a file")
	}
	if got.pendingAttachmentPath != filePath {
		t.Fatalf("pendingAttachmentPath = %q, want %q", got.pendingAttachmentPath, filePath)
	}
	if got.input != "" {
		t.Fatalf("input = %q, want empty caption composer", got.input)
	}
}

func TestFilePickerBackspaceGoesToParentDirectory(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}

	model := m{
		status:         "ready",
		mode:           "chat",
		active:         "15551230001@s.whatsapp.net",
		whitelist:      map[string]string{"15551230001": "Allowed"},
		fileBrowserDir: child,
	}
	if err := model.loadFileBrowserDir(child); err != nil {
		t.Fatal(err)
	}
	model.fileBrowserOpen = true

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyBackspace})
	got := next.(m)

	if got.fileBrowserDir != root {
		t.Fatalf("fileBrowserDir = %q, want %q", got.fileBrowserDir, root)
	}
}

func TestAttachmentEnterSendsFileWithCaptionInDemoMode(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "photo.jpg")
	if err := os.WriteFile(filePath, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}

	model := m{
		status:                "ready",
		mode:                  "chat",
		active:                "15551230001@s.whatsapp.net",
		whitelist:             map[string]string{"15551230001": "Allowed"},
		demoMode:              true,
		pendingAttachmentPath: filePath,
		pendingAttachmentKind: "image",
		pendingAttachmentName: "photo.jpg",
		input:                 "summer trip",
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyEnter})
	armed := next.(m)
	next, cmd := armed.Update(composerSendMsg{seq: armed.pendingSendSeq})
	got := next.(m)

	if cmd == nil {
		t.Fatal("expected top bar command for demo media send")
	}
	if got.pendingAttachmentPath != "" {
		t.Fatal("expected pending attachment to clear after send attempt")
	}
}

func TestEscCancelsPendingAttachment(t *testing.T) {
	model := m{
		status:                "ready",
		mode:                  "chat",
		active:                "15551230001@s.whatsapp.net",
		whitelist:             map[string]string{"15551230001": "Allowed"},
		pendingAttachmentPath: "C:\\tmp\\photo.jpg",
		pendingAttachmentKind: "image",
		pendingAttachmentName: "photo.jpg",
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(m)

	if got.pendingAttachmentPath != "" {
		t.Fatal("expected Esc to clear pending attachment")
	}
}

func TestNameFallsBackToIndexedContactByNumber(t *testing.T) {
	model := m{
		contacts: map[string]contact{
			"15551230001@lid": {ID: "15551230001@lid", Notify: "Alex Alias"},
		},
	}
	model.rebuildContactIndex()

	got := model.name(chat{ID: "15551230001@s.whatsapp.net"})
	if got != "Alex Alias" {
		t.Fatalf("name() = %q, want %q", got, "Alex Alias")
	}
}

func TestSidebarItemsContactsCacheInvalidatesOnIdentityChange(t *testing.T) {
	model := m{
		sidebarTab:   "contacts",
		sidebarCache: &sidebarCache{},
		contacts: map[string]contact{
			"15550000002@s.whatsapp.net": {ID: "15550000002@s.whatsapp.net", Notify: "Bravo"},
			"15550000001@s.whatsapp.net": {ID: "15550000001@s.whatsapp.net", Notify: "Alpha"},
		},
	}
	model.rebuildContactIndex()
	model.markIdentityChanged()

	first := model.sidebarItems()
	if len(first) != 2 || first[0].ID != "15550000001@s.whatsapp.net" {
		t.Fatalf("unexpected initial sidebar ordering: %+v", first)
	}

	model.names = map[string]string{"15550000002": "Aaron"}
	model.markIdentityChanged()

	second := model.sidebarItems()
	if len(second) != 2 || second[0].ID != "15550000002@s.whatsapp.net" {
		t.Fatalf("sidebar cache did not refresh after identity change: %+v", second)
	}
}

func TestRenderMainIncomingMessageKeepsBodyColorAfterPrefix(t *testing.T) {
	currentTheme = Monokai
	rehashStyles()

	msg := wireMsg{
		Message: map[string]any{"conversation": "hello there"},
	}
	msg.Key.ID = "m1"
	msg.Key.RemoteJID = "120363040000001234@g.us"
	msg.Key.Participant = "15551230001@s.whatsapp.net"
	msg.MessageTimestamp = 1710000000

	model := m{
		active:           msg.Key.RemoteJID,
		msgs:             map[string][]wireMsg{msg.Key.RemoteJID: {msg}},
		contacts:         map[string]contact{"15551230001@s.whatsapp.net": {ID: "15551230001@s.whatsapp.net", Notify: "annu"}},
		contactsByNumber: map[string]contact{"15551230001": {ID: "15551230001@s.whatsapp.net", Notify: "annu"}},
		whitelist:        map[string]string{},
		names:            map[string]string{},
	}

	rendered := model.renderMain(60, 6)
	prefix := lipgloss.NewStyle().Foreground(receivedName).Bold(true).Render("annu: ")
	body := lipgloss.NewStyle().Foreground(receivedText).Render("hello there")

	if !strings.Contains(rendered, prefix) {
		t.Fatalf("rendered message missing received-name prefix styling: %q", rendered)
	}
	if !strings.Contains(rendered, body) {
		t.Fatalf("rendered message missing received-text styling after prefix: %q", rendered)
	}
}

func TestFileOpenMsgReportsFailure(t *testing.T) {
	model := m{}

	next, cmd := model.Update(fileOpenMsg{path: "C:\\missing.txt", err: fmt.Errorf("shell failed")})
	got := next.(m)
	if cmd == nil {
		t.Fatal("expected top bar command on file open failure")
	}
	followUp := cmd()
	final, _ := got.Update(followUp)
	updated := final.(m)
	if updated.topBarMsg != "Open failed: shell failed" {
		t.Fatalf("topBarMsg = %q, want %q", updated.topBarMsg, "Open failed: shell failed")
	}
}

func TestRenderMainQuoteUsesSentColorsWhenQuotingMe(t *testing.T) {
	currentTheme = Monokai
	rehashStyles()

	msg := wireMsg{
		Message: map[string]any{
			"extendedTextMessage": map[string]any{
				"text":              "reply text",
				"quotedText":        "my old message",
				"quotedParticipant": "",
			},
		},
	}
	msg.Key.ID = "m2"
	msg.Key.RemoteJID = "15551230001@s.whatsapp.net"
	msg.MessageTimestamp = 1710000000

	model := m{
		active:           msg.Key.RemoteJID,
		msgs:             map[string][]wireMsg{msg.Key.RemoteJID: {msg}},
		contacts:         map[string]contact{msg.Key.RemoteJID: {ID: msg.Key.RemoteJID, Notify: "annu"}},
		contactsByNumber: map[string]contact{"15551230001": {ID: msg.Key.RemoteJID, Notify: "annu"}},
	}

	rendered := model.renderMain(80, 8)
	quotePrefix := lipgloss.NewStyle().Foreground(sentName).Bold(true).Render("Me: ")
	quoteBody := lipgloss.NewStyle().Foreground(sentText).Render("my old message")
	if !strings.Contains(rendered, quotePrefix) || !strings.Contains(rendered, quoteBody) {
		t.Fatalf("self-quote did not use sent colors: %q", rendered)
	}
}

func TestRenderMainQuoteUsesReceivedColorsWhenQuotingOtherSender(t *testing.T) {
	currentTheme = Monokai
	rehashStyles()

	msg := wireMsg{
		Message: map[string]any{
			"extendedTextMessage": map[string]any{
				"text":              "reply text",
				"quotedText":        "their old message",
				"quotedParticipant": "15551230001@s.whatsapp.net",
			},
		},
	}
	msg.Key.ID = "m3"
	msg.Key.RemoteJID = "15551230002@s.whatsapp.net"
	msg.MessageTimestamp = 1710000000

	model := m{
		active: msg.Key.RemoteJID,
		msgs:   map[string][]wireMsg{msg.Key.RemoteJID: {msg}},
		contacts: map[string]contact{
			"15551230001@s.whatsapp.net": {ID: "15551230001@s.whatsapp.net", Notify: "bob"},
			"15551230002@s.whatsapp.net": {ID: "15551230002@s.whatsapp.net", Notify: "annu"},
		},
		contactsByNumber: map[string]contact{
			"15551230001": {ID: "15551230001@s.whatsapp.net", Notify: "bob"},
			"15551230002": {ID: "15551230002@s.whatsapp.net", Notify: "annu"},
		},
	}

	rendered := model.renderMain(80, 8)
	quotePrefix := lipgloss.NewStyle().Foreground(receivedName).Bold(true).Render("bob: ")
	quoteBody := lipgloss.NewStyle().Foreground(receivedText).Render("their old message")
	if !strings.Contains(rendered, quotePrefix) || !strings.Contains(rendered, quoteBody) {
		t.Fatalf("received quote did not use received colors: %q", rendered)
	}
}

func TestRenderReplyBarTruncatesToPaneWidth(t *testing.T) {
	currentTheme = Monokai
	rehashStyles()

	reply := &wireMsg{
		Message: map[string]any{
			"conversation": "Are you planning to attend tomorrow? The first lecture?",
		},
	}
	reply.Key.ID = "q1"
	reply.Key.RemoteJID = "15551230001@s.whatsapp.net"

	model := m{
		replyTo: reply,
		contacts: map[string]contact{
			"15551230001@s.whatsapp.net": {ID: "15551230001@s.whatsapp.net", Notify: "Shadu"},
		},
		contactsByNumber: map[string]contact{
			"15551230001": {ID: "15551230001@s.whatsapp.net", Notify: "Shadu"},
		},
	}

	rendered := model.renderReplyBar(140, 32)
	if lipgloss.Width(rendered) != 32 {
		t.Fatalf("reply bar width = %d, want 32, rendered=%q", lipgloss.Width(rendered), rendered)
	}
	if !strings.Contains(rendered, "Esc cancel") {
		t.Fatalf("reply bar missing cancel hint: %q", rendered)
	}
	if !strings.Contains(rendered, "...") {
		t.Fatalf("reply bar should truncate long quoted text: %q", rendered)
	}
}

func TestViewReplyBarDoesNotExpandPastFrameWidth(t *testing.T) {
	currentTheme = Monokai
	rehashStyles()

	reply := &wireMsg{
		Message: map[string]any{
			"conversation": "Are you planning to attend tomorrow? The first lecture?",
		},
	}
	reply.Key.ID = "q2"
	reply.Key.RemoteJID = "15551230001@s.whatsapp.net"

	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		w:         120,
		h:         8,
		replyTo:   reply,
		whitelist: map[string]string{"15551230001": "Shadu"},
		contacts: map[string]contact{
			"15551230001@s.whatsapp.net": {ID: "15551230001@s.whatsapp.net", Notify: "Shadu"},
		},
		contactsByNumber: map[string]contact{
			"15551230001": {ID: "15551230001@s.whatsapp.net", Notify: "Shadu"},
		},
		msgs: map[string][]wireMsg{
			"15551230001@s.whatsapp.net": {},
		},
		sidebarCache: &sidebarCache{},
		mainCache:    &renderCache{},
	}

	rendered := model.View()
	for _, line := range strings.Split(rendered, "\n") {
		if lipgloss.Width(line) > model.w {
			t.Fatalf("view line width = %d, want <= %d, line=%q", lipgloss.Width(line), model.w, line)
		}
	}
}

func TestRenderMainOutgoingReactionKeepsMessageIndentedRight(t *testing.T) {
	currentTheme = Monokai
	rehashStyles()

	msg := wireMsg{
		Message:          map[string]any{"conversation": "big data. SPM later"},
		MessageTimestamp: 1710000000,
	}
	msg.Key.ID = "m4"
	msg.Key.RemoteJID = "15551230001@s.whatsapp.net"
	msg.Key.FromMe = true

	rxn := wireMsg{
		Message: map[string]any{
			"reactionMessage": map[string]any{
				"targetMsgID": "m4",
				"emoji":       "thumbs_up",
			},
		},
		MessageTimestamp: 1710000001,
	}
	rxn.Key.ID = "r1"
	rxn.Key.RemoteJID = msg.Key.RemoteJID
	rxn.Key.Participant = "15551230001@s.whatsapp.net"

	model := m{
		active: msg.Key.RemoteJID,
		msgs:   map[string][]wireMsg{msg.Key.RemoteJID: {msg, rxn}},
		contacts: map[string]contact{
			"15551230001@s.whatsapp.net": {ID: "15551230001@s.whatsapp.net", Notify: "Shadu Mady"},
		},
		contactsByNumber: map[string]contact{
			"15551230001": {ID: "15551230001@s.whatsapp.net", Notify: "Shadu Mady"},
		},
	}

	rendered := model.renderMain(100, 8)
	reactionPrefix := lipgloss.NewStyle().Foreground(receivedName).Render("  ╰─ ")
	reactionBody := lipgloss.NewStyle().Foreground(receivedName).Bold(true).Render("thumbs_up Shadu Mady")
	lines := strings.Split(rendered, "\n")
	foundMsg := false
	foundReaction := false
	for _, line := range lines {
		if strings.Contains(line, "big data. SPM later") {
			foundMsg = true
			if !strings.HasPrefix(line, " ") {
				t.Fatalf("outgoing reacted line lost right indent: %q", line)
			}
		}
		if strings.Contains(line, "Shadu Mady") {
			foundReaction = true
			if !strings.Contains(line, "╰─") {
				t.Fatalf("reaction line missing linked prefix: %q", line)
			}
			if !strings.HasPrefix(line, " ") {
				t.Fatalf("reaction line lost right indent: %q", line)
			}
		}
	}
	if !foundMsg {
		t.Fatalf("failed to find reacted outgoing line in render: %q", rendered)
	}
	if !foundReaction {
		t.Fatalf("failed to find outgoing reaction line in render: %q", rendered)
	}
	if !strings.Contains(rendered, reactionPrefix) {
		t.Fatalf("reaction link did not use reactor name color: %q", rendered)
	}
	if !strings.Contains(rendered, reactionBody) {
		t.Fatalf("reaction body did not use reactor name color: %q", rendered)
	}
}

func TestRenderUserListHighlightedNameUsesMarqueeOffset(t *testing.T) {
	currentTheme = Monokai
	rehashStyles()

	model := m{
		mode:                 "nav",
		sidebarFocused:       true,
		sel:                  0,
		sidebarMarqueeOffset: 4,
		whitelist:            map[string]string{"15551230001": "Very Long Highlighted Contact Name"},
		contacts: map[string]contact{
			"15551230001@s.whatsapp.net": {ID: "15551230001@s.whatsapp.net", Notify: "Very Long Highlighted Contact Name"},
		},
		contactsByNumber: map[string]contact{
			"15551230001": {ID: "15551230001@s.whatsapp.net", Notify: "Very Long Highlighted Contact Name"},
		},
	}
	items := []chat{{ID: "15551230001@s.whatsapp.net"}}

	lines := model.renderUserList(items, 0, 1, 30)
	if len(lines) == 0 {
		t.Fatal("renderUserList returned no lines")
	}
	if !strings.Contains(lines[0], "Long Highl") {
		t.Fatalf("highlighted row did not render marquee window: %q", lines[0])
	}
	if strings.Contains(lines[0], "1. Very Long Highlighted") {
		t.Fatalf("highlighted row ignored marquee offset: %q", lines[0])
	}
	if got := lipgloss.Width(lines[0]); got != 28 {
		t.Fatalf("highlighted row width = %d, want 28", got)
	}
}

func TestSidebarHighlightInsetAnimatesToOne(t *testing.T) {
	model := m{
		mode:           "nav",
		sidebarFocused: true,
		sidebarTab:     "chats",
		sel:            0,
		chats:          []chat{{ID: "15551230001@s.whatsapp.net", ConversationTimestamp: 1710000000}},
	}

	model.syncSidebarHighlight()
	if model.sidebarHighlightInset != 0 {
		t.Fatalf("initial sidebarHighlightInset = %d, want 0", model.sidebarHighlightInset)
	}

	model.advanceSidebarHighlight()
	if model.sidebarHighlightInset != 1 {
		t.Fatalf("animated sidebarHighlightInset = %d, want 1", model.sidebarHighlightInset)
	}
}

func TestChatEnterSchedulesDeferredSend(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{"15551230001": "Allowed"},
		msgs:      map[string][]wireMsg{},
		input:     "hello now",
	}

	next, cmd := model.key(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(m)

	if cmd == nil {
		t.Fatal("expected deferred send command")
	}
	if !got.pendingSendArmed {
		t.Fatal("expected pending send to be armed")
	}
	msgs := got.msgs[got.active]
	if len(msgs) != 0 {
		t.Fatalf("optimistic send appended %d messages before timer, want 0", len(msgs))
	}
}

func TestDeferredSendCommitsOutgoingMessage(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{"15551230001": "Allowed"},
		msgs:      map[string][]wireMsg{},
		input:     "hello now",
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyEnter})
	armed := next.(m)
	next, _ = armed.Update(composerSendMsg{seq: armed.pendingSendSeq})
	got := next.(m)

	msgs := got.msgs[got.active]
	if len(msgs) != 1 {
		t.Fatalf("optimistic send appended %d messages, want 1", len(msgs))
	}
	if !msgs[0].Key.FromMe {
		t.Fatalf("optimistic message should be from me: %+v", msgs[0].Key)
	}
	if !strings.HasPrefix(msgs[0].Key.ID, "local-") {
		t.Fatalf("optimistic message id = %q, want local-*", msgs[0].Key.ID)
	}
	if body, _ := msgs[0].Message["conversation"].(string); body != "hello now" {
		t.Fatalf("optimistic message body = %q, want %q", body, "hello now")
	}
}

func TestAltEnterAddsComposerNewlineWithoutSending(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{"15551230001": "Allowed"},
		msgs:      map[string][]wireMsg{},
		input:     "hello",
	}

	next, cmd := model.key(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	got := next.(m)

	if cmd != nil {
		t.Fatalf("expected no send command, got %#v", cmd)
	}
	if got.input != "hello\n" {
		t.Fatalf("input = %q, want %q", got.input, "hello\n")
	}
	if len(got.msgs[got.active]) != 0 {
		t.Fatalf("expected no optimistic messages, got %d", len(got.msgs[got.active]))
	}
}

func TestPastedEnterAddsComposerNewlineWithoutSending(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{"15551230001": "Allowed"},
		msgs:      map[string][]wireMsg{},
		input:     "hello",
	}

	next, cmd := model.key(tea.KeyMsg{Type: tea.KeyEnter, Paste: true})
	got := next.(m)

	if cmd != nil {
		t.Fatalf("expected no send command, got %#v", cmd)
	}
	if got.input != "hello\n" {
		t.Fatalf("input = %q, want %q", got.input, "hello\n")
	}
	if len(got.msgs[got.active]) != 0 {
		t.Fatalf("expected no optimistic messages, got %d", len(got.msgs[got.active]))
	}
}

func TestChatEnterSendsMultilineDraftAsOneMessage(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{"15551230001": "Allowed"},
		msgs:      map[string][]wireMsg{},
		input:     "hello\nworld",
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyEnter})
	armed := next.(m)
	next, _ = armed.Update(composerSendMsg{seq: armed.pendingSendSeq})
	got := next.(m)

	msgs := got.msgs[got.active]
	if len(msgs) != 1 {
		t.Fatalf("optimistic send appended %d messages, want 1", len(msgs))
	}
	if body, _ := msgs[0].Message["conversation"].(string); body != "hello\nworld" {
		t.Fatalf("optimistic multiline body = %q, want %q", body, "hello\nworld")
	}
}

func TestPasteLikeEnterAfterBulkRunesArmsDeferredSend(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{"15551230001": "Allowed"},
		msgs:      map[string][]wireMsg{},
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	got := next.(m)
	if got.inputBuf == "" {
		t.Fatalf("expected buffered input after bulk runes")
	}

	next, cmd := got.key(tea.KeyMsg{Type: tea.KeyEnter})
	got = next.(m)

	if cmd == nil {
		t.Fatal("expected deferred send command")
	}
	if !got.pendingSendArmed {
		t.Fatal("expected pending send to be armed")
	}
	if len(got.msgs[got.active]) != 0 {
		t.Fatalf("expected no messages before deferred send, got %d", len(got.msgs[got.active]))
	}
}

func TestPastedMultilineBurstDoesNotSendUntilFinalEnter(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{"15551230001": "Allowed"},
		msgs:      map[string][]wireMsg{},
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line one")})
	state := next.(m)
	next, _ = state.key(tea.KeyMsg{Type: tea.KeyEnter})
	state = next.(m)
	next, _ = state.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line two")})
	state = next.(m)

	if state.input != "line one\n" {
		t.Fatalf("input after paste burst = %q, want %q", state.input, "line one\n")
	}
	if state.inputBuf != "line two" {
		t.Fatalf("inputBuf after paste burst = %q, want %q", state.inputBuf, "line two")
	}
	if len(state.msgs[state.active]) != 0 {
		t.Fatalf("expected no messages sent during paste burst, got %d", len(state.msgs[state.active]))
	}

	next, _ = state.key(tea.KeyMsg{Type: tea.KeyEnter})
	state = next.(m)
	next, _ = state.Update(composerSendMsg{seq: state.pendingSendSeq})
	state = next.(m)

	msgs := state.msgs[state.active]
	if len(msgs) != 1 {
		t.Fatalf("sent %d messages, want 1", len(msgs))
	}
	if body, _ := msgs[0].Message["conversation"].(string); body != "line one\nline two" {
		t.Fatalf("sent body = %q, want %q", body, "line one\nline two")
	}
}

func TestRunCommandSoundProfileAndToggle(t *testing.T) {
	t.Setenv("WHATZAP_DATA_DIR", t.TempDir())
	currentConfig = Config{
		ThemeName:    "tokyonight",
		MouseEnabled: true,
		SoundEnabled: true,
		SoundProfile: 2,
	}

	model := m{
		soundEnabled: true,
		soundProfile: 2,
	}

	cmd, ok := model.runCommand("/sound4", true)
	if !ok || cmd == nil {
		t.Fatalf("expected /sound4 to be handled with preview command")
	}
	if !model.soundEnabled || model.soundProfile != 4 {
		t.Fatalf("model sound state after /sound4 = enabled:%v profile:%d, want enabled:true profile:4", model.soundEnabled, model.soundProfile)
	}
	if !currentConfig.SoundEnabled || currentConfig.SoundProfile != 4 {
		t.Fatalf("config sound state after /sound4 = enabled:%v profile:%d, want enabled:true profile:4", currentConfig.SoundEnabled, currentConfig.SoundProfile)
	}

	cmd, ok = model.runCommand("/soundoff", true)
	if !ok || cmd == nil {
		t.Fatalf("expected /soundoff to be handled")
	}
	if model.soundEnabled {
		t.Fatalf("model sound should be disabled after /soundoff")
	}
	if currentConfig.SoundEnabled {
		t.Fatalf("config sound should be disabled after /soundoff")
	}

	cmd, ok = model.runCommand("/soundon", true)
	if !ok || cmd == nil {
		t.Fatalf("expected /soundon to be handled with preview command")
	}
	if !model.soundEnabled || model.soundProfile != 4 {
		t.Fatalf("model sound state after /soundon = enabled:%v profile:%d, want enabled:true profile:4", model.soundEnabled, model.soundProfile)
	}
	if !currentConfig.SoundEnabled || currentConfig.SoundProfile != 4 {
		t.Fatalf("config sound state after /soundon = enabled:%v profile:%d, want enabled:true profile:4", currentConfig.SoundEnabled, currentConfig.SoundProfile)
	}
}

func TestNavListWrapAround(t *testing.T) {
	model := m{
		status: "ready",
		mode:   "nav",
		chats: []chat{
			{ID: "a@s.whatsapp.net", ConversationTimestamp: 3},
			{ID: "b@s.whatsapp.net", ConversationTimestamp: 2},
			{ID: "c@s.whatsapp.net", ConversationTimestamp: 1},
		},
		sel: 0,
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyUp})
	got := next.(m)
	if got.sel != 2 {
		t.Fatalf("nav up wrap sel = %d, want 2", got.sel)
	}

	next, _ = got.key(tea.KeyMsg{Type: tea.KeyDown})
	got = next.(m)
	if got.sel != 0 {
		t.Fatalf("nav down wrap sel = %d, want 0", got.sel)
	}
}

func TestSearchListWrapAround(t *testing.T) {
	model := m{
		status: "ready",
		mode:   "search",
		chats: []chat{
			{ID: "a@s.whatsapp.net", ConversationTimestamp: 3},
			{ID: "b@s.whatsapp.net", ConversationTimestamp: 2},
		},
		sel: 0,
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyUp})
	got := next.(m)
	if got.sel != 1 {
		t.Fatalf("search up wrap sel = %d, want 1", got.sel)
	}

	next, _ = got.key(tea.KeyMsg{Type: tea.KeyDown})
	got = next.(m)
	if got.sel != 0 {
		t.Fatalf("search down wrap sel = %d, want 0", got.sel)
	}
}

func TestRefreshWindowTitleCmdTracksUnreadCounts(t *testing.T) {
	model := m{
		contacts: map[string]contact{
			"a@s.whatsapp.net": {ID: "a@s.whatsapp.net", Notify: "Alice Johnson"},
			"b@s.whatsapp.net": {ID: "b@s.whatsapp.net", Notify: "Bob Stone"},
			"c@s.whatsapp.net": {ID: "c@s.whatsapp.net", Notify: "Charlie Fox"},
		},
		contactsByNumber: map[string]contact{},
		chats: []chat{
			{ID: "a@s.whatsapp.net", UnreadCount: 3},
			{ID: "b@s.whatsapp.net", UnreadCount: 2},
			{ID: "c@s.whatsapp.net", UnreadCount: 1},
		},
	}

	cmd := model.refreshWindowTitleCmd()
	if cmd == nil {
		t.Fatalf("expected title update command for unread chats")
	}
	if model.windowTitle != "WhatZap (🟢 Alice,Bob,Charlie)" {
		t.Fatalf("windowTitle = %q, want %q", model.windowTitle, "WhatZap (🟢 Alice,Bob,Charlie)")
	}

	cmd = model.refreshWindowTitleCmd()
	if cmd != nil {
		t.Fatalf("expected no-op title command when title is unchanged")
	}

	model.chats[0].UnreadCount = 0
	model.chats[1].UnreadCount = 0
	model.chats[2].UnreadCount = 0
	cmd = model.refreshWindowTitleCmd()
	if cmd == nil {
		t.Fatalf("expected title reset command when unread count clears")
	}
	if model.windowTitle != "WhatZap" {
		t.Fatalf("windowTitle = %q, want %q", model.windowTitle, "WhatZap")
	}
}

func TestRefreshWindowTitleCmdShowsOnlyFirstThreeNames(t *testing.T) {
	model := m{
		contacts: map[string]contact{
			"a@s.whatsapp.net": {ID: "a@s.whatsapp.net", Notify: "Alice Johnson"},
			"b@s.whatsapp.net": {ID: "b@s.whatsapp.net", Notify: "Bob Stone"},
			"c@s.whatsapp.net": {ID: "c@s.whatsapp.net", Notify: "Charlie Fox"},
			"d@s.whatsapp.net": {ID: "d@s.whatsapp.net", Notify: "Dora Reed"},
		},
		contactsByNumber: map[string]contact{},
		chats: []chat{
			{ID: "a@s.whatsapp.net", UnreadCount: 1},
			{ID: "b@s.whatsapp.net", UnreadCount: 1},
			{ID: "c@s.whatsapp.net", UnreadCount: 1},
			{ID: "d@s.whatsapp.net", UnreadCount: 1},
		},
	}

	_ = model.refreshWindowTitleCmd()
	if model.windowTitle != "WhatZap (🟢 Alice,Bob,Charlie +1)" {
		t.Fatalf("windowTitle = %q, want %q", model.windowTitle, "WhatZap (🟢 Alice,Bob,Charlie +1)")
	}
}

func TestHeaderAvatarInitials(t *testing.T) {
	if got := headerAvatarInitials("Alice Brown", "15551230001@s.whatsapp.net"); got != "AB" {
		t.Fatalf("headerAvatarInitials() = %q, want %q", got, "AB")
	}
	if got := headerAvatarInitials("alice", "15551230001@s.whatsapp.net"); got != "A " {
		t.Fatalf("headerAvatarInitials() single = %q, want %q", got, "A ")
	}
	if got := headerAvatarInitials("", "15551230001@s.whatsapp.net"); got != "01" {
		t.Fatalf("headerAvatarInitials() fallback = %q, want %q", got, "01")
	}
}
