package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "modernc.org/sqlite"
)

func newTestApp(t *testing.T) *App {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS chat_permissions (
		phone   TEXT PRIMARY KEY,
		name    TEXT NOT NULL DEFAULT '',
		allowed INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		t.Fatalf("create chat_permissions: %v", err)
	}

	cacheFile, err := os.CreateTemp("", "whatzap-state-*.json")
	if err != nil {
		t.Fatalf("create cache file: %v", err)
	}
	cachePath := cacheFile.Name()
	if err := cacheFile.Close(); err != nil {
		t.Fatalf("close cache file: %v", err)
	}
	t.Cleanup(func() {
		for i := 0; i < 20; i++ {
			if err := os.Remove(cachePath); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})

	return &App{
		db:        db,
		cachePath: cachePath,
		wsClients: map[*websocket.Conn]struct{}{},
		state: PersistedState{
			Chats:    map[string]Chat{},
			Contacts: map[string]Contact{},
			Messages: map[string][]WireMessage{},
		},
	}
}

// Receipt check.
func TestUpdateReceiptStatus(t *testing.T) {
	app := newTestApp(t)
	app.state.Messages["chat-1"] = []WireMessage{
		{Key: WireKey{ID: "m1", FromMe: true}, ReceiptStatus: "sent"},
		{Key: WireKey{ID: "m2", FromMe: true}, ReceiptStatus: "read"},
		{Key: WireKey{ID: "m3", FromMe: false}},
	}

	changed := app.updateReceiptStatus("chat-1", []string{"m1", "m2", "m3"}, "delivered")
	if !changed {
		t.Fatalf("expected receipt update to change state")
	}
	if got := app.state.Messages["chat-1"][0].ReceiptStatus; got != "delivered" {
		t.Fatalf("m1 receipt = %q, want delivered", got)
	}
	if got := app.state.Messages["chat-1"][1].ReceiptStatus; got != "read" {
		t.Fatalf("m2 receipt regressed to %q", got)
	}
	if got := app.state.Messages["chat-1"][2].ReceiptStatus; got != "" {
		t.Fatalf("received message receipt changed to %q", got)
	}
}

// Timestamp check.
func TestReconcileChatTimestamps(t *testing.T) {
	state := &PersistedState{
		Chats: map[string]Chat{
			"chat-1": {ID: "chat-1", ConversationTimestamp: 10},
			"chat-2": {ID: "chat-2", ConversationTimestamp: 200},
		},
		Contacts: map[string]Contact{},
		Messages: map[string][]WireMessage{
			"chat-1": {
				{MessageTimestamp: 40},
				{MessageTimestamp: 90},
			},
			"chat-2": {
				{MessageTimestamp: 150},
			},
			"chat-3": {
				{MessageTimestamp: 77},
			},
		},
	}

	changed := reconcileChatTimestamps(state)
	if !changed {
		t.Fatalf("expected timestamp reconciliation to report changes")
	}
	if got := state.Chats["chat-1"].ConversationTimestamp; got != 90 {
		t.Fatalf("chat-1 timestamp = %d, want 90", got)
	}
	if got := state.Chats["chat-2"].ConversationTimestamp; got != 200 {
		t.Fatalf("chat-2 timestamp = %d, want 200", got)
	}
	if got := state.Chats["chat-3"].ConversationTimestamp; got != 77 {
		t.Fatalf("chat-3 timestamp = %d, want 77", got)
	}
}

// Dedupe check.
func TestUpsertMessageDedupesAndKeepsUnreadStable(t *testing.T) {
	app := newTestApp(t)

	first := WireMessage{
		Key:              WireKey{ID: "m1", FromMe: false},
		MessageTimestamp: 100,
		Message:          map[string]any{"conversation": "first"},
		PushName:         "Alex",
	}
	second := WireMessage{
		Key:              WireKey{ID: "m1", FromMe: false},
		MessageTimestamp: 120,
		Message:          map[string]any{"conversation": "updated"},
		PushName:         "Alex",
	}

	app.upsertMessage("15551230001@s.whatsapp.net", first)
	app.upsertMessage("15551230001@s.whatsapp.net", second)

	msgs := app.state.Messages["15551230001@s.whatsapp.net"]
	if len(msgs) != 1 {
		t.Fatalf("message count = %d, want 1", len(msgs))
	}
	if got, _ := msgs[0].Message["conversation"].(string); got != "updated" {
		t.Fatalf("message body = %q, want updated", got)
	}
	if got := app.state.Chats["15551230001@s.whatsapp.net"].UnreadCount; got != 1 {
		t.Fatalf("unread count = %d, want 1", got)
	}
}

// Logout success.
func TestHandleLogoutSuccess(t *testing.T) {
	app := newTestApp(t)
	app.started = true
	app.connected = true
	app.state = PersistedState{
		Chats:    map[string]Chat{"chat-1": {ID: "chat-1"}},
		Contacts: map[string]Contact{"chat-1": {ID: "chat-1"}},
		Messages: map[string][]WireMessage{"chat-1": {{Key: WireKey{ID: "m1"}}}},
	}

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	rec := httptest.NewRecorder()
	app.handleLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if app.started || app.connected {
		t.Fatalf("app flags not cleared after logout")
	}
	if len(app.state.Chats) != 0 || len(app.state.Contacts) != 0 || len(app.state.Messages) != 0 {
		t.Fatalf("app state was not cleared")
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse logout response: %v", err)
	}
	if got := body["message"]; got != "Logged out successfully" {
		t.Fatalf("logout message = %#v, want Logged out successfully", got)
	}

	raw, err := os.ReadFile(app.cachePath)
	if err != nil {
		t.Fatalf("failed to read persisted state: %v", err)
	}
	var persisted PersistedState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("failed to parse persisted state: %v", err)
	}
	if len(persisted.Chats) != 0 || len(persisted.Contacts) != 0 || len(persisted.Messages) != 0 {
		t.Fatalf("persisted state was not cleared")
	}
}

// Logout failure.
func TestHandleLogoutFailsWhenStateCannotBePersisted(t *testing.T) {
	app := newTestApp(t)
	app.cachePath = t.TempDir()
	app.started = true
	app.connected = true
	app.state = PersistedState{
		Chats:    map[string]Chat{"chat-1": {ID: "chat-1"}},
		Contacts: map[string]Contact{"chat-1": {ID: "chat-1"}},
		Messages: map[string][]WireMessage{"chat-1": {{Key: WireKey{ID: "m1"}}}},
	}

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	rec := httptest.NewRecorder()
	app.handleLogout(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "state cleanup failed") {
		t.Fatalf("unexpected error body: %s", rec.Body.String())
	}
}

// Save check.
func TestPersistStateWithErr(t *testing.T) {
	app := newTestApp(t)
	app.state = PersistedState{
		Chats: map[string]Chat{
			"15551230001@s.whatsapp.net": {ID: "15551230001@s.whatsapp.net", Name: "Alex", ConversationTimestamp: 42},
		},
		Contacts: map[string]Contact{
			"15551230001@s.whatsapp.net": {ID: "15551230001@s.whatsapp.net", Notify: "Alex"},
		},
		Messages: map[string][]WireMessage{
			"15551230001@s.whatsapp.net": {
				{Key: WireKey{ID: "m1", RemoteJID: "15551230001@s.whatsapp.net"}, MessageTimestamp: 42, Message: map[string]any{"conversation": "hello"}},
			},
		},
	}

	if err := app.persistStateWithErr(); err != nil {
		t.Fatalf("persist state failed: %v", err)
	}

	raw, err := os.ReadFile(app.cachePath)
	if err != nil {
		t.Fatalf("read persisted state failed: %v", err)
	}

	var persisted PersistedState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("parse persisted state failed: %v", err)
	}
	if got := persisted.Chats["15551230001@s.whatsapp.net"].Name; got != "Alex" {
		t.Fatalf("chat name = %q, want Alex", got)
	}
	if got := persisted.Messages["15551230001@s.whatsapp.net"][0].Key.ID; got != "m1" {
		t.Fatalf("message id = %q, want m1", got)
	}
}

// Load check.
func TestLoadStateInitializesMapsAndReconcilesTimestamps(t *testing.T) {
	app := newTestApp(t)
	state := `{"chats":{"15551230001":{"id":"15551230001","name":"Alex"}},"messages":{"15551230001":[{"key":{"remoteJid":"15551230001","fromMe":false},"message":{"conversation":"hello"},"messageTimestamp":77}]}}`
	if err := os.WriteFile(app.cachePath, []byte(state), 0o644); err != nil {
		t.Fatalf("write state file failed: %v", err)
	}

	app.loadState()

	if app.needsBootstrapSync {
		t.Fatalf("expected loadState to avoid bootstrap mode")
	}
	if app.state.Contacts == nil {
		t.Fatalf("contacts map was not initialized")
	}
	chat, ok := app.state.Chats["15551230001@s.whatsapp.net"]
	if !ok {
		t.Fatalf("canonical chat id was not loaded")
	}
	if chat.ConversationTimestamp != 77 {
		t.Fatalf("chat timestamp = %d, want 77", chat.ConversationTimestamp)
	}
	msgs := app.state.Messages["15551230001@s.whatsapp.net"]
	if len(msgs) != 1 {
		t.Fatalf("message count = %d, want 1", len(msgs))
	}
	if msgs[0].Key.RemoteJID != "15551230001@s.whatsapp.net" {
		t.Fatalf("remote jid = %q, want canonical jid", msgs[0].Key.RemoteJID)
	}
}

// Health check.
func TestHandleHealth(t *testing.T) {
	app := newTestApp(t)
	app.connected = true

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	app.handleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse health response: %v", err)
	}
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("health ok field not true")
	}
	if connected, _ := body["connected"].(bool); !connected {
		t.Fatalf("health connected field not true")
	}
}

// Chat list check.
func TestHandleChatsAndContacts(t *testing.T) {
	app := newTestApp(t)
	app.state.Chats["15551230001@s.whatsapp.net"] = Chat{ID: "15551230001@s.whatsapp.net", ConversationTimestamp: 10}
	app.state.Chats["15551230002@s.whatsapp.net"] = Chat{ID: "15551230002@s.whatsapp.net", Name: "Zed", ConversationTimestamp: 20}
	app.state.Contacts["15551230001@s.whatsapp.net"] = Contact{ID: "15551230001@s.whatsapp.net", Notify: "Alex"}

	chatsReq := httptest.NewRequest(http.MethodGet, "/chats", nil)
	chatsRec := httptest.NewRecorder()
	app.handleChats(chatsRec, chatsReq)

	if chatsRec.Code != http.StatusOK {
		t.Fatalf("chats status = %d, want %d", chatsRec.Code, http.StatusOK)
	}
	var chatsBody struct {
		Chats []Chat `json:"chats"`
	}
	if err := json.Unmarshal(chatsRec.Body.Bytes(), &chatsBody); err != nil {
		t.Fatalf("parse chats response: %v", err)
	}
	if len(chatsBody.Chats) != 2 {
		t.Fatalf("chat count = %d, want 2", len(chatsBody.Chats))
	}
	if chatsBody.Chats[1].Name != "Alex" && chatsBody.Chats[0].Name != "Alex" {
		t.Fatalf("expected contact name to hydrate chat name")
	}

	contactsReq := httptest.NewRequest(http.MethodGet, "/contacts", nil)
	contactsRec := httptest.NewRecorder()
	app.handleContacts(contactsRec, contactsReq)

	if contactsRec.Code != http.StatusOK {
		t.Fatalf("contacts status = %d, want %d", contactsRec.Code, http.StatusOK)
	}
	var contactsBody struct {
		Contacts []Contact `json:"contacts"`
	}
	if err := json.Unmarshal(contactsRec.Body.Bytes(), &contactsBody); err != nil {
		t.Fatalf("parse contacts response: %v", err)
	}
	if len(contactsBody.Contacts) != 1 {
		t.Fatalf("contact count = %d, want 1", len(contactsBody.Contacts))
	}
}

// Whitelist check.
func TestWhitelistRoundTrip(t *testing.T) {
	app := newTestApp(t)

	setReq := httptest.NewRequest(http.MethodPost, "/whitelist/set", bytes.NewBufferString(`{"phone":"15551230001","name":"Alex","allowed":1}`))
	setRec := httptest.NewRecorder()
	app.handleSetWhitelist(setRec, setReq)

	if setRec.Code != http.StatusOK {
		t.Fatalf("set whitelist status = %d, want %d, body=%s", setRec.Code, http.StatusOK, setRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/whitelist", nil)
	getRec := httptest.NewRecorder()
	app.handleGetWhitelist(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("get whitelist status = %d, want %d", getRec.Code, http.StatusOK)
	}
	var body struct {
		Contacts []struct {
			Phone   string `json:"phone"`
			Name    string `json:"name"`
			Allowed int    `json:"allowed"`
		} `json:"contacts"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse whitelist response: %v", err)
	}
	if len(body.Contacts) != 1 {
		t.Fatalf("whitelist count = %d, want 1", len(body.Contacts))
	}
	if body.Contacts[0].Phone != "15551230001" || body.Contacts[0].Allowed != 1 {
		t.Fatalf("unexpected whitelist entry: %+v", body.Contacts[0])
	}
}

// Route validation.
func TestCoreRouteValidation(t *testing.T) {
	app := newTestApp(t)

	sendFileReq := httptest.NewRequest(http.MethodPost, "/messages/send-file", bytes.NewBufferString(`{"chatId":"15551230001@s.whatsapp.net","path":"missing.txt","kind":"document"}`))
	sendFileRec := httptest.NewRecorder()
	app.handleSendFile(sendFileRec, sendFileReq)
	if sendFileRec.Code != http.StatusBadRequest || !strings.Contains(sendFileRec.Body.String(), "file not found") {
		t.Fatalf("unexpected send-file response: %d %s", sendFileRec.Code, sendFileRec.Body.String())
	}

	messagesReq := httptest.NewRequest(http.MethodGet, "/messages", nil)
	messagesRec := httptest.NewRecorder()
	app.handleMessages(messagesRec, messagesReq)
	if messagesRec.Code != http.StatusBadRequest || !strings.Contains(messagesRec.Body.String(), "chatId is required") {
		t.Fatalf("unexpected messages response: %d %s", messagesRec.Code, messagesRec.Body.String())
	}

	setWhitelistReq := httptest.NewRequest(http.MethodGet, "/whitelist/set", nil)
	setWhitelistRec := httptest.NewRecorder()
	app.handleSetWhitelist(setWhitelistRec, setWhitelistReq)
	if setWhitelistRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("whitelist method status = %d, want %d", setWhitelistRec.Code, http.StatusMethodNotAllowed)
	}
}

// File validation.
func TestValidateSendFileInput(t *testing.T) {
	imagePath := filepath.Join(t.TempDir(), "sample.png")
	if err := os.WriteFile(imagePath, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 0o644); err != nil {
		t.Fatalf("write image file: %v", err)
	}
	if err := validateSendFileInput(imagePath, "image"); err != nil {
		t.Fatalf("image validation failed: %v", err)
	}

	videoPath := filepath.Join(t.TempDir(), "sample.mp4")
	if err := os.WriteFile(videoPath, []byte("not-a-real-video"), 0o644); err != nil {
		t.Fatalf("write video file: %v", err)
	}
	if err := validateSendFileInput(videoPath, "video"); err != nil {
		t.Fatalf("video validation failed: %v", err)
	}

	textPath := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(textPath, []byte("plain text"), 0o644); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	if err := validateSendFileInput(textPath, "image"); err == nil {
		t.Fatalf("expected image validation to fail for text file")
	}
	if err := validateSendFileInput(textPath, "video"); err == nil {
		t.Fatalf("expected video validation to fail for text file")
	}
	if err := validateSendFileInput(textPath, "document"); err != nil {
		t.Fatalf("document validation failed: %v", err)
	}
}
