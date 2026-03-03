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
