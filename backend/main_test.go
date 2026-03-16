package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	waCommon "go.mau.fi/whatsmeow/proto/waCommon"
	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	waHistorySync "go.mau.fi/whatsmeow/proto/waHistorySync"
	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

func newTestApp(t *testing.T) *App {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
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

	app := &App{
		db:        db,
		cachePath: cachePath,
		apiToken:  "test-api-token",
		wsClients: map[*websocket.Conn]struct{}{},
		state: PersistedState{
			Chats:    map[string]Chat{},
			Contacts: map[string]Contact{},
			Messages: map[string][]WireMessage{},
		},
	}
	t.Cleanup(func() {
		if app.db != nil {
			_ = app.db.Close()
		}
	})
	return app
}

func authorizedRequest(req *http.Request, app *App) *http.Request {
	req.Header.Set(authHeaderName, "Bearer "+app.apiToken)
	return req
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

func TestUpsertMessageKeepsNewestConversationTimestamp(t *testing.T) {
	app := newTestApp(t)
	chatID := "15551230001@s.whatsapp.net"

	app.upsertMessage(chatID, WireMessage{
		Key:              WireKey{ID: "newest", FromMe: false},
		MessageTimestamp: 200,
		Message:          map[string]any{"conversation": "latest"},
	})
	app.upsertMessage(chatID, WireMessage{
		Key:              WireKey{ID: "older", FromMe: false},
		MessageTimestamp: 100,
		Message:          map[string]any{"conversation": "older"},
	})
	app.upsertMessage(chatID, WireMessage{
		Key:              WireKey{ID: "zero", FromMe: false},
		MessageTimestamp: 0,
		Message:          map[string]any{"conversation": "unknown"},
	})

	if got := app.state.Chats[chatID].ConversationTimestamp; got != 200 {
		t.Fatalf("conversation timestamp = %d, want 200", got)
	}
}

func TestApplyHistorySyncDoesNotDoubleCountUnread(t *testing.T) {
	app := newTestApp(t)
	chatID := "15551230001@s.whatsapp.net"

	history := &waHistorySync.HistorySync{
		Conversations: []*waHistorySync.Conversation{
			{
				ID:                    proto.String(chatID),
				ConversationTimestamp: proto.Uint64(200),
				LastMsgTimestamp:      proto.Uint64(200),
				UnreadCount:           proto.Uint32(3),
				Messages: []*waHistorySync.HistorySyncMsg{
					{
						Message: &waWeb.WebMessageInfo{
							Key: &waCommon.MessageKey{
								ID:        proto.String("m1"),
								RemoteJID: proto.String(chatID),
								FromMe:    proto.Bool(false),
							},
							MessageTimestamp: proto.Uint64(100),
							Message: &waE2E.Message{
								Conversation: proto.String("older 1"),
							},
						},
					},
					{
						Message: &waWeb.WebMessageInfo{
							Key: &waCommon.MessageKey{
								ID:        proto.String("m2"),
								RemoteJID: proto.String(chatID),
								FromMe:    proto.Bool(false),
							},
							MessageTimestamp: proto.Uint64(150),
							Message: &waE2E.Message{
								Conversation: proto.String("older 2"),
							},
						},
					},
				},
			},
		},
	}

	app.applyHistorySync(history)

	if got := app.state.Chats[chatID].UnreadCount; got != 3 {
		t.Fatalf("unread count = %d, want 3", got)
	}
}

// Logout success.
func TestUpsertMessageSkipsPersistDuringHistorySync(t *testing.T) {
	app := newTestApp(t)
	app.historySyncing = true

	msg := WireMessage{
		Key:              WireKey{ID: "m1", FromMe: false},
		MessageTimestamp: 100,
		Message:          map[string]any{"conversation": "first"},
	}

	app.upsertMessage("15551230001@s.whatsapp.net", msg)
	time.Sleep(50 * time.Millisecond)

	raw, err := os.ReadFile(app.cachePath)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("cache file was written during history sync: %q", string(raw))
	}

	app.historySyncing = false
	app.upsertMessage("15551230001@s.whatsapp.net", WireMessage{
		Key:              WireKey{ID: "m2", FromMe: false},
		MessageTimestamp: 101,
		Message:          map[string]any{"conversation": "second"},
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, err = os.ReadFile(app.cachePath)
		if err == nil && len(raw) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for persisted state")
		}
		time.Sleep(20 * time.Millisecond)
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

func TestHandleLogoutRemovesLocalCacheArtifacts(t *testing.T) {
	app := newTestApp(t)
	app.cacheDir = t.TempDir()
	app.cachePath = filepath.Join(app.cacheDir, "state.json")
	if err := os.WriteFile(app.cachePath, []byte(`{"stale":true}`), 0o644); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	leftover := filepath.Join(app.cacheDir, "leftover.tmp")
	if err := os.WriteFile(leftover, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write leftover file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	rec := httptest.NewRecorder()
	app.handleLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("leftover file still exists or stat failed: %v", err)
	}
	raw, err := os.ReadFile(app.cachePath)
	if err != nil {
		t.Fatalf("failed to read recreated state file: %v", err)
	}
	var persisted PersistedState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("failed to parse recreated state file: %v", err)
	}
	if len(persisted.Chats) != 0 || len(persisted.Contacts) != 0 || len(persisted.Messages) != 0 {
		t.Fatalf("recreated persisted state was not empty")
	}
	if app.db != nil {
		_ = app.db.Close()
		app.db = nil
	}
}

func TestHandlerRejectsUnauthorizedRequests(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/contacts", nil)
	rec := httptest.NewRecorder()

	app.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestHandlerAllowsAuthorizedRequests(t *testing.T) {
	app := newTestApp(t)
	req := authorizedRequest(httptest.NewRequest(http.MethodGet, "/contacts", nil), app)
	rec := httptest.NewRecorder()

	app.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHealthDoesNotRequireAuth(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	app.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestCORSAllowsOnlyLoopbackOrigins(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodOptions, "/contacts", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()

	app.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("allow origin = %q, want localhost origin", got)
	}
}

func TestCORSRejectsNonLoopbackOrigins(t *testing.T) {
	app := newTestApp(t)
	req := httptest.NewRequest(http.MethodOptions, "/contacts", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()

	app.handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestWebSocketRequiresAuthAndAllowedOrigin(t *testing.T) {
	app := newTestApp(t)
	srv := httptest.NewServer(app.handler())
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"

	t.Run("missing auth is rejected", func(t *testing.T) {
		header := http.Header{}
		header.Set("Origin", "http://localhost:3000")
		_, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err == nil {
			t.Fatalf("expected websocket dial to fail without auth")
		}
	})

	t.Run("disallowed origin is rejected", func(t *testing.T) {
		header := http.Header{}
		header.Set(authHeaderName, "Bearer "+app.apiToken)
		header.Set("Origin", "https://evil.example")
		_, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err == nil {
			t.Fatalf("expected websocket dial to fail for disallowed origin")
		}
	})

	t.Run("authorized loopback origin succeeds", func(t *testing.T) {
		header := http.Header{}
		header.Set(authHeaderName, "Bearer "+app.apiToken)
		header.Set("Origin", "http://127.0.0.1:3000")
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			t.Fatalf("expected websocket dial to succeed: %v", err)
		}
		_ = conn.Close()
	})
}

// Logout failure.
func TestHandleLogoutFailsWhenStateCannotBePersisted(t *testing.T) {
	app := newTestApp(t)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker file failed: %v", err)
	}
	app.cachePath = filepath.Join(blocker, "state.json")
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

// Atomic write check.
func TestWriteFileAtomicReplacesFileAndCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file failed: %v", err)
	}

	if err := writeFileAtomic(path, []byte("new"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic failed: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read target file failed: %v", err)
	}
	if string(raw) != "new" {
		t.Fatalf("file contents = %q, want new", string(raw))
	}

	matches, err := filepath.Glob(filepath.Join(dir, "state.json.tmp-*"))
	if err != nil {
		t.Fatalf("glob temp files failed: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no temp files, found %d", len(matches))
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

func TestLoadStateCorruptionFallsBackToBootstrap(t *testing.T) {
	app := newTestApp(t)
	app.state.Chats["stale"] = Chat{ID: "stale"}
	if err := os.WriteFile(app.cachePath, []byte("{not-json"), 0o644); err != nil {
		t.Fatalf("write corrupt state file failed: %v", err)
	}

	app.loadState()

	if !app.needsBootstrapSync {
		t.Fatalf("expected bootstrap mode after corrupt state")
	}
	if len(app.state.Chats) != 0 || len(app.state.Contacts) != 0 || len(app.state.Messages) != 0 {
		t.Fatalf("expected in-memory state reset after corrupt state")
	}
	if _, err := os.Stat(app.cachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected corrupt cache file to be removed, got err=%v", err)
	}
}

// Cache path check.
func TestResolveBackendCacheDirUsesOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "appdata")
	t.Setenv("WHATZAP_DATA_DIR", override)

	cacheDir, err := resolveBackendCacheDir(t.TempDir())
	if err != nil {
		t.Fatalf("resolve backend cache dir: %v", err)
	}

	want := filepath.Join(override, "backend")
	if cacheDir != want {
		t.Fatalf("cache dir = %q, want %q", cacheDir, want)
	}
	if _, err := os.Stat(cacheDir); err != nil {
		t.Fatalf("cache dir was not created: %v", err)
	}
}

// Legacy migration check.
func TestResolveBackendCacheDirMigratesLegacyCache(t *testing.T) {
	override := filepath.Join(t.TempDir(), "appdata")
	t.Setenv("WHATZAP_DATA_DIR", override)

	workDir := t.TempDir()
	legacyDir := filepath.Join(workDir, ".whatsmeow_cache")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	statePath := filepath.Join(legacyDir, "state.json")
	dbPath := filepath.Join(legacyDir, "store.db")
	if err := os.WriteFile(statePath, []byte(`{"chats":{"c1":{"id":"c1"}}}`), 0o644); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("sqlite-bytes"), 0o644); err != nil {
		t.Fatalf("write legacy db: %v", err)
	}

	cacheDir, err := resolveBackendCacheDir(workDir)
	if err != nil {
		t.Fatalf("resolve backend cache dir: %v", err)
	}

	migratedState, err := os.ReadFile(filepath.Join(cacheDir, "state.json"))
	if err != nil {
		t.Fatalf("read migrated state: %v", err)
	}
	if string(migratedState) != `{"chats":{"c1":{"id":"c1"}}}` {
		t.Fatalf("unexpected migrated state: %s", string(migratedState))
	}
	migratedDB, err := os.ReadFile(filepath.Join(cacheDir, "store.db"))
	if err != nil {
		t.Fatalf("read migrated db: %v", err)
	}
	if string(migratedDB) != "sqlite-bytes" {
		t.Fatalf("unexpected migrated db: %s", string(migratedDB))
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

func TestUpsertPermissionSkipsNilDB(t *testing.T) {
	app := newTestApp(t)
	_ = app.db.Close()
	app.db = nil

	app.upsertPermission("15551230001", "Alex")
}

func TestUpsertPermissionSkipsDuringShutdown(t *testing.T) {
	app := newTestApp(t)
	app.mu.Lock()
	app.shuttingDown = true
	app.mu.Unlock()

	app.upsertPermission("15551230001", "Alex")

	rows, err := app.db.Query(`SELECT phone, name, allowed FROM chat_permissions`)
	if err != nil {
		t.Fatalf("query permissions: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatalf("expected no permission rows during shutdown")
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
	if sendFileRec.Code != http.StatusConflict || !strings.Contains(sendFileRec.Body.String(), "not connected") {
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

func TestHandleMessagesCapsLimit(t *testing.T) {
	app := newTestApp(t)
	chatID := "15551230001@s.whatsapp.net"
	msgs := make([]WireMessage, 0, 250)
	for i := 0; i < 250; i++ {
		msgs = append(msgs, WireMessage{
			Key:              WireKey{ID: fmt.Sprintf("m-%03d", i), RemoteJID: chatID},
			MessageTimestamp: int64(i + 1),
			Message:          map[string]any{"conversation": fmt.Sprintf("msg %03d", i)},
		})
	}
	app.state.Messages[chatID] = msgs

	req := httptest.NewRequest(http.MethodGet, "/messages?chatId="+chatID+"&limit=9999", nil)
	rec := httptest.NewRecorder()
	app.handleMessages(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var body struct {
		Messages []WireMessage `json:"messages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse messages response: %v", err)
	}
	if len(body.Messages) != maxMessagesResponseLimit {
		t.Fatalf("message count = %d, want %d", len(body.Messages), maxMessagesResponseLimit)
	}
	if body.Messages[0].Key.ID != "m-050" {
		t.Fatalf("first returned message = %q, want m-050", body.Messages[0].Key.ID)
	}
	if body.Messages[len(body.Messages)-1].Key.ID != "m-249" {
		t.Fatalf("last returned message = %q, want m-249", body.Messages[len(body.Messages)-1].Key.ID)
	}
}

func TestHandlersRequiringWhatsAppConnectionReturnConflictWhenDisconnected(t *testing.T) {
	app := newTestApp(t)
	app.state.Messages["15551230001@s.whatsapp.net"] = []WireMessage{
		{
			Key:        WireKey{ID: "m1", RemoteJID: "15551230001@s.whatsapp.net"},
			MediaProto: "ZmFrZQ==",
		},
	}

	tests := []struct {
		name string
		req  *http.Request
		run  func(http.ResponseWriter, *http.Request)
	}{
		{
			name: "send message",
			req:  httptest.NewRequest(http.MethodPost, "/messages/send", bytes.NewBufferString(`{"chatId":"15551230001@s.whatsapp.net","text":"hi"}`)),
			run:  app.handleSendMessage,
		},
		{
			name: "send file",
			req:  httptest.NewRequest(http.MethodPost, "/messages/send-file", bytes.NewBufferString(`{"chatId":"15551230001@s.whatsapp.net","path":"sample.txt","kind":"document"}`)),
			run:  app.handleSendFile,
		},
		{
			name: "profile picture",
			req:  httptest.NewRequest(http.MethodGet, "/profile-picture?jid=15551230001@s.whatsapp.net", nil),
			run:  app.handleProfilePicture,
		},
		{
			name: "media download",
			req:  httptest.NewRequest(http.MethodGet, "/media/download?chatId=15551230001@s.whatsapp.net&msgId=m1", nil),
			run:  app.handleMediaDownload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.run(rec, tt.req)
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusConflict, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "not connected") {
				t.Fatalf("unexpected body: %s", rec.Body.String())
			}
		})
	}
}

func TestNormalizeQuotedParticipantClearsSelfID(t *testing.T) {
	got := normalizeQuotedParticipant("15551230001@lid", "15551230001@s.whatsapp.net", func(id string) string {
		switch strings.TrimSpace(id) {
		case "15551230001@lid":
			return "15551230001@s.whatsapp.net"
		default:
			return strings.TrimSpace(id)
		}
	})
	if got != "" {
		t.Fatalf("normalizeQuotedParticipant() = %q, want empty", got)
	}
}

func TestNormalizeQuotedParticipantClearsSelfIDByPhoneMatch(t *testing.T) {
	got := normalizeQuotedParticipant("15551230001:7@s.whatsapp.net", "15551230001@s.whatsapp.net", func(id string) string {
		return strings.TrimSpace(id)
	})
	if got != "" {
		t.Fatalf("normalizeQuotedParticipant() by phone match = %q, want empty", got)
	}
}

func TestQuotedFromMeForOneToOneChatTreatsNonRemoteAsSelf(t *testing.T) {
	if !quotedFromMeForChat("15551230001@s.whatsapp.net", "15559990000@s.whatsapp.net", false) {
		t.Fatalf("expected 1:1 quoted participant different from remote chat to be treated as self")
	}
	if quotedFromMeForChat("15551230001@s.whatsapp.net", "15551230001@s.whatsapp.net", false) {
		t.Fatalf("expected remote participant in 1:1 chat to stay non-self")
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
