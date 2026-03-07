package main

import (
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
	msg.Key.RemoteJID = "15551230001@s.whatsapp.net"
	msg.MessageTimestamp = 1710000000

	model := m{
		active:           msg.Key.RemoteJID,
		msgs:             map[string][]wireMsg{msg.Key.RemoteJID: {msg}},
		contacts:         map[string]contact{msg.Key.RemoteJID: {ID: msg.Key.RemoteJID, Notify: "annu"}},
		contactsByNumber: map[string]contact{"15551230001": {ID: msg.Key.RemoteJID, Notify: "annu"}},
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
	quoteBody := lipgloss.NewStyle().Foreground(sentText).Italic(true).Render("my old message")
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
	quoteBody := lipgloss.NewStyle().Foreground(receivedText).Italic(true).Render("their old message")
	if !strings.Contains(rendered, quotePrefix) || !strings.Contains(rendered, quoteBody) {
		t.Fatalf("received quote did not use received colors: %q", rendered)
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

func TestChatEnterAppendsOptimisticOutgoingMessageImmediately(t *testing.T) {
	model := m{
		status:    "ready",
		mode:      "chat",
		active:    "15551230001@s.whatsapp.net",
		whitelist: map[string]string{"15551230001": "Allowed"},
		msgs:      map[string][]wireMsg{},
		input:     "hello now",
	}

	next, _ := model.key(tea.KeyMsg{Type: tea.KeyEnter})
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
