package main

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gorilla/websocket"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	waHistorySync "go.mau.fi/whatsmeow/proto/waHistorySync"
	waWeb "go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	_ "modernc.org/sqlite"
)

type EventEnvelope struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

type WireKey struct {
	ID          string `json:"id"`
	RemoteJID   string `json:"remoteJid"`
	FromMe      bool   `json:"fromMe"`
	Participant string `json:"participant,omitempty"`
}

type WireMessage struct {
	Key              WireKey        `json:"key"`
	Message          map[string]any `json:"message"`
	MessageTimestamp int64          `json:"messageTimestamp"`
	PushName         string         `json:"pushName,omitempty"`
	MediaProto       string         `json:"mediaProto,omitempty"` // base64 proto bytes for media messages
	ReceiptStatus    string         `json:"receiptStatus,omitempty"`
}

type WireReceiptUpdate struct {
	ChatID        string   `json:"chatId"`
	MessageIDs    []string `json:"messageIds"`
	ReceiptStatus string   `json:"receiptStatus"`
}

type WireCallEvent struct {
	Status   string `json:"status"`
	CallerID string `json:"callerId,omitempty"`
	GroupID  string `json:"groupId,omitempty"`
	CallID   string `json:"callId,omitempty"`
	Media    string `json:"media,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type Chat struct {
	ID                    string `json:"id"`
	Name                  string `json:"name,omitempty"`
	Subject               string `json:"subject,omitempty"`
	ConversationTimestamp int64  `json:"conversationTimestamp"`
	UnreadCount           int    `json:"unreadCount"`
}

type Contact struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Notify string `json:"notify,omitempty"`
}

type PersistedState struct {
	Chats    map[string]Chat          `json:"chats"`
	Contacts map[string]Contact       `json:"contacts"`
	Messages map[string][]WireMessage `json:"messages"`
}

type App struct {
	mu sync.RWMutex

	client         *whatsmeow.Client
	db             *sql.DB
	storeContainer *sqlstore.Container
	started        bool
	connected      bool

	cacheDir  string
	cachePath string
	state     PersistedState

	apiToken string

	wsMu      sync.Mutex
	wsClients map[*websocket.Conn]struct{}

	needsBootstrapSync bool
	historySyncing     bool
	shuttingDown       bool
}

const maxUploadBytes = 100 * 1024 * 1024
const maxMessagesResponseLimit = 200
const appDataDirName = "WhatZAP"
const apiTokenEnvVar = "WHATZAP_API_TOKEN"
const authHeaderName = "Authorization"

func main() {
	app, err := NewApp()
	if err != nil {
		log.Fatalf("init failed: %v", err)
	}

	addr := "127.0.0.1:8787"
	log.Printf("whatsmeow backend listening on http://%s", addr)
	if err := http.ListenAndServe(addr, app.handler()); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func whatzapDataRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv("WHATZAP_DATA_DIR")); override != "" {
		return filepath.Clean(override), nil
	}
	if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
		return filepath.Join(base, appDataDirName), nil
	}
	if base, err := os.UserConfigDir(); err == nil && strings.TrimSpace(base) != "" {
		return filepath.Join(base, appDataDirName), nil
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".whatzap"), nil
	}
	return "", fmt.Errorf("failed to resolve app data directory")
}

func resolveBackendCacheDir(workDir string) (string, error) {
	dataRoot, err := whatzapDataRoot()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(dataRoot, "backend")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
	}

	legacyDir := filepath.Join(workDir, ".whatsmeow_cache")
	shouldMigrate, err := shouldMigrateLegacyDir(cacheDir, legacyDir)
	if err != nil {
		return "", err
	}
	if shouldMigrate {
		if err := copyDirContents(legacyDir, cacheDir); err != nil {
			return "", err
		}
	}

	return cacheDir, nil
}

func shouldMigrateLegacyDir(targetDir, legacyDir string) (bool, error) {
	if filepath.Clean(targetDir) == filepath.Clean(legacyDir) {
		return false, nil
	}
	info, err := os.Stat(legacyDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func copyDirContents(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, info.Mode().Perm()); err != nil {
				return err
			}
			if err := copyDirContents(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := copyFile(srcPath, dstPath, info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(srcPath, dstPath string, perm os.FileMode) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}
	return nil
}

func NewApp() (*App, error) {
	apiToken := strings.TrimSpace(os.Getenv(apiTokenEnvVar))
	if apiToken == "" {
		return nil, fmt.Errorf("%s must be set", apiTokenEnvVar)
	}
	workDir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cacheDir, err := resolveBackendCacheDir(workDir)
	if err != nil {
		return nil, err
	}
	cachePath := filepath.Join(cacheDir, "state.json")
	app := &App{
		cacheDir:  cacheDir,
		cachePath: cachePath,
		apiToken:  apiToken,
		wsClients: map[*websocket.Conn]struct{}{},
		state: PersistedState{
			Chats:    map[string]Chat{},
			Contacts: map[string]Contact{},
			Messages: map[string][]WireMessage{},
		},
	}
	if err := app.initPersistentResources(); err != nil {
		return nil, err
	}
	app.loadState()
	return app, nil
}

func (a *App) initPersistentResources() error {
	cacheDir := strings.TrimSpace(a.cacheDir)
	if cacheDir == "" {
		if a.cachePath != "" {
			cacheDir = filepath.Dir(a.cachePath)
		} else {
			return fmt.Errorf("cache directory is not configured")
		}
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	a.cacheDir = cacheDir
	a.cachePath = filepath.Join(cacheDir, "state.json")

	dbPath := filepath.Join(cacheDir, "store.db")
	container, err := sqlstore.New(context.Background(), "sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)", waLog.Stdout("db", "WARN", true))
	if err != nil {
		return err
	}
	device, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return err
	}

	rawDB, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=journal_mode(WAL)")
	if err != nil {
		return err
	}
	if _, err := rawDB.Exec(`CREATE TABLE IF NOT EXISTS chat_permissions (
		phone   TEXT PRIMARY KEY,
		name    TEXT NOT NULL DEFAULT '',
		allowed INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		_ = rawDB.Close()
		return err
	}

	a.db = rawDB
	a.storeContainer = container
	a.client = whatsmeow.NewClient(device, waLog.Stdout("client", "WARN", true))
	a.bindEvents()
	return nil
}

func (a *App) resetPersistentStorage() error {
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			return err
		}
		a.db = nil
	}

	if strings.TrimSpace(a.cacheDir) != "" {
		if err := os.RemoveAll(a.cacheDir); err != nil {
			return err
		}
		return a.initPersistentResources()
	}

	if strings.TrimSpace(a.cachePath) != "" {
		if err := os.Remove(a.cachePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (a *App) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", a.handleHealth)
	mux.HandleFunc("/ws", a.handleWS)
	mux.HandleFunc("/start", a.handleStart)
	mux.HandleFunc("/chats", a.handleChats)
	mux.HandleFunc("/contacts", a.handleContacts)
	mux.HandleFunc("/resolve/lidpn", a.handleResolveLIDPN)
	mux.HandleFunc("/sync/contacts", a.handleSyncContacts)
	mux.HandleFunc("/sync/groups", a.handleSyncGroups)
	mux.HandleFunc("/messages", a.handleMessages)
	mux.HandleFunc("/messages/send", a.handleSendMessage)
	mux.HandleFunc("/messages/send-file", a.handleSendFile)
	mux.HandleFunc("/messages/read", a.handleMarkRead)
	mux.HandleFunc("/profile-picture", a.handleProfilePicture)
	mux.HandleFunc("/logout", a.handleLogout)
	mux.HandleFunc("/whitelist", a.handleGetWhitelist)
	mux.HandleFunc("/whitelist/set", a.handleSetWhitelist)
	mux.HandleFunc("/names/set", a.handleSetName)
	mux.HandleFunc("/media/download", a.handleMediaDownload)
	return withCORS(a.withAuth(mux))
}

func (a *App) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if !a.isAuthorized(r) {
			writeErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) isAuthorized(r *http.Request) bool {
	authz := strings.TrimSpace(r.Header.Get(authHeaderName))
	if !strings.HasPrefix(authz, "Bearer ") {
		return false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authz, "Bearer "))
	if token == "" || a.apiToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(a.apiToken)) == 1
}

func isAllowedOrigin(origin string) bool {
	if strings.TrimSpace(origin) == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (a *App) bindEvents() {
	a.client.AddEventHandler(func(evt interface{}) {
		switch v := evt.(type) {
		case *events.Connected:
			a.mu.Lock()
			a.connected = true
			a.mu.Unlock()
			a.broadcast(EventEnvelope{Type: "ready"})
			a.broadcast(EventEnvelope{Type: "chats:loaded"})
		case *events.Disconnected:
			a.mu.Lock()
			a.connected = false
			a.mu.Unlock()
			a.broadcast(EventEnvelope{Type: "disconnected", Payload: "connection closed"})
		case *events.LoggedOut:
			a.mu.Lock()
			a.connected = false
			a.started = false
			a.mu.Unlock()
			a.broadcast(EventEnvelope{Type: "status", Payload: "Logged out. Start again to scan QR."})
			if a.client != nil && a.client.Store != nil {
				_ = a.client.Store.Delete(context.Background())
				a.client.Store.ID = nil
			}
		case *events.PushName:
			jid := a.canonicalizeChatID(v.JID.String())
			a.mu.Lock()
			a.state.Contacts[jid] = Contact{ID: jid, Notify: v.NewPushName}
			a.mu.Unlock()
			a.persistState()
			a.broadcast(EventEnvelope{Type: "contacts:updated"})
		case *events.Message:
			if v == nil || v.Message == nil {
				return
			}
			chatID := a.canonicalizeChatID(v.Info.Chat.String())
			if chatID == "" || chatID == "status@broadcast" {
				return
			}
			msg := a.toWireMessage(v)
			msg.Key.RemoteJID = chatID
			if msg.Key.Participant != "" {
				msg.Key.Participant = a.canonicalizeChatID(msg.Key.Participant)
			}
			a.upsertMessage(chatID, msg)
			a.broadcast(EventEnvelope{Type: "message", Payload: msg})
			a.broadcast(EventEnvelope{Type: "chats:loaded"})
		case *events.Receipt:
			if v == nil {
				return
			}
			chatID := a.canonicalizeChatID(v.Chat.String())
			status := receiptStatusFromType(v.Type)
			if chatID == "" || status == "" || len(v.MessageIDs) == 0 {
				return
			}
			ids := make([]string, 0, len(v.MessageIDs))
			for _, id := range v.MessageIDs {
				if id != "" {
					ids = append(ids, string(id))
				}
			}
			if len(ids) == 0 {
				return
			}
			if a.updateReceiptStatus(chatID, ids, status) {
				a.broadcast(EventEnvelope{Type: "receipt", Payload: WireReceiptUpdate{
					ChatID:        chatID,
					MessageIDs:    ids,
					ReceiptStatus: status,
				}})
			}
		case *events.HistorySync:
			a.applyHistorySync(v.Data)
		case *events.CallOffer:
			a.broadcast(EventEnvelope{Type: "call", Payload: a.toWireCallEvent("incoming", v.BasicCallMeta, "")})
		case *events.CallOfferNotice:
			a.broadcast(EventEnvelope{Type: "call", Payload: a.toWireCallEvent("incoming", v.BasicCallMeta, v.Media)})
		case *events.CallTerminate:
			ev := a.toWireCallEvent("ended", v.BasicCallMeta, "")
			ev.Reason = strings.TrimSpace(v.Reason)
			a.broadcast(EventEnvelope{Type: "call", Payload: ev})
		}
	})
}

func (a *App) toWireCallEvent(status string, meta types.BasicCallMeta, media string) WireCallEvent {
	callerID := a.canonicalizeChatID(meta.CallCreator.String())
	if callerID == "" {
		callerID = a.canonicalizeChatID(meta.CallCreatorAlt.String())
	}
	if callerID == "" {
		callerID = a.canonicalizeChatID(meta.From.String())
	}
	groupID := a.canonicalizeChatID(meta.GroupJID.String())
	return WireCallEvent{
		Status:   status,
		CallerID: callerID,
		GroupID:  groupID,
		CallID:   meta.CallID,
		Media:    strings.TrimSpace(media),
	}
}

func (a *App) applyHistorySync(data *waHistorySync.HistorySync) {
	if data == nil {
		return
	}

	a.mu.Lock()
	a.historySyncing = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.historySyncing = false
		a.mu.Unlock()
	}()

	changedChats := false

	a.mu.Lock()
	for _, p := range data.GetPushnames() {
		id := a.canonicalizeChatID(strings.TrimSpace(p.GetID()))
		if id == "" {
			continue
		}
		contact := a.state.Contacts[id]
		contact.ID = id
		if name := strings.TrimSpace(p.GetPushname()); name != "" {
			contact.Notify = name
		}
		a.state.Contacts[id] = contact
		if contact.Notify != "" {
			chat := a.state.Chats[id]
			chat.ID = id
			if chat.Name == "" {
				chat.Name = contact.Notify
			}
			a.state.Chats[id] = chat
		}
	}

	for _, conv := range data.GetConversations() {
		chatID := a.historyConversationChatID(conv)
		if chatID == "" || chatID == "status@broadcast" {
			continue
		}
		chat := a.state.Chats[chatID]
		chat.ID = chatID
		if name := strings.TrimSpace(conv.GetDisplayName()); name != "" {
			chat.Name = name
		} else if name := strings.TrimSpace(conv.GetName()); name != "" {
			chat.Name = name
		}
		if chat.Name == "" {
			if ct, ok := a.state.Contacts[chatID]; ok {
				if n := strings.TrimSpace(ct.Notify); n != "" {
					chat.Name = n
				} else if n := strings.TrimSpace(ct.Name); n != "" {
					chat.Name = n
				}
			}
		}
		if ts := int64(conv.GetConversationTimestamp()); ts > chat.ConversationTimestamp {
			chat.ConversationTimestamp = ts
		}
		if chat.ConversationTimestamp == 0 {
			if ts := int64(conv.GetLastMsgTimestamp()); ts > 0 {
				chat.ConversationTimestamp = ts
			}
		}
		if uc := int(conv.GetUnreadCount()); uc > chat.UnreadCount {
			chat.UnreadCount = uc
		}
		a.state.Chats[chatID] = chat
		changedChats = true
	}
	a.mu.Unlock()

	for _, conv := range data.GetConversations() {
		chatID := a.historyConversationChatID(conv)
		if chatID == "" || chatID == "status@broadcast" {
			continue
		}
		for _, item := range conv.GetMessages() {
			wm := item.GetMessage()
			if wm == nil {
				continue
			}
			msg := a.toWireMessageFromHistory(wm)
			if msg.Key.RemoteJID == "" {
				msg.Key.RemoteJID = chatID
			}
			msg.Key.RemoteJID = a.canonicalizeChatID(msg.Key.RemoteJID)
			if msg.Key.Participant != "" {
				msg.Key.Participant = a.canonicalizeChatID(msg.Key.Participant)
			}
			if msg.Key.RemoteJID == "status@broadcast" {
				continue
			}
			a.upsertMessage(msg.Key.RemoteJID, msg)
			changedChats = true
		}
	}

	if changedChats {
		a.persistState()
		a.broadcast(EventEnvelope{Type: "chats:loaded"})
		a.broadcast(EventEnvelope{Type: "contacts:updated"})
	}
}

func (a *App) upsertMessage(chatID string, msg WireMessage) {
	chatID = a.canonicalizeChatID(chatID)
	if chatID == "" {
		return
	}
	msg.Key.RemoteJID = chatID
	if msg.Key.Participant != "" {
		msg.Key.Participant = a.canonicalizeChatID(msg.Key.Participant)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	list := a.state.Messages[chatID]
	key := dedupeKey(msg)
	found := -1
	for i := range list {
		if dedupeKey(list[i]) == key {
			found = i
			break
		}
	}
	isNew := found < 0
	if found >= 0 {
		list[found] = msg
	} else {
		list = append(list, msg)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].MessageTimestamp < list[j].MessageTimestamp
	})
	if len(list) > 200 {
		list = list[len(list)-200:]
	}
	a.state.Messages[chatID] = list

	chat := a.state.Chats[chatID]
	chat.ID = chatID
	if msg.MessageTimestamp > chat.ConversationTimestamp {
		chat.ConversationTimestamp = msg.MessageTimestamp
	}
	if isNew && !msg.Key.FromMe && !a.historySyncing {
		chat.UnreadCount++
	}
	if chat.Name == "" && msg.PushName != "" && !msg.Key.FromMe && !strings.HasSuffix(chatID, "@g.us") {
		chat.Name = msg.PushName
	}
	a.state.Chats[chatID] = chat

	if msg.PushName != "" && !msg.Key.FromMe && !strings.HasSuffix(chatID, "@g.us") {
		a.state.Contacts[chatID] = Contact{ID: chatID, Notify: msg.PushName}
	}
	if !a.historySyncing {
		go a.persistState()
	}
	go a.upsertPermission(phoneFromJID(chatID), msg.PushName)
}

func receiptStatusFromType(t types.ReceiptType) string {
	switch t {
	case types.ReceiptTypeDelivered, types.ReceiptTypeSender:
		return "delivered"
	case types.ReceiptTypeRead, types.ReceiptTypeReadSelf:
		return "read"
	case types.ReceiptTypePlayed, types.ReceiptTypePlayedSelf:
		return "played"
	default:
		return ""
	}
}

func receiptStatusRank(status string) int {
	switch status {
	case "sent":
		return 1
	case "delivered":
		return 2
	case "read":
		return 3
	case "played":
		return 4
	default:
		return 0
	}
}

func (a *App) updateReceiptStatus(chatID string, ids []string, status string) bool {
	if receiptStatusRank(status) == 0 {
		return false
	}
	idSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id != "" {
			idSet[id] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	list := a.state.Messages[chatID]
	changed := false
	for i := range list {
		if !list[i].Key.FromMe {
			continue
		}
		if _, ok := idSet[list[i].Key.ID]; !ok {
			continue
		}
		if receiptStatusRank(status) > receiptStatusRank(list[i].ReceiptStatus) {
			list[i].ReceiptStatus = status
			changed = true
		}
	}
	if changed {
		a.state.Messages[chatID] = list
		go a.persistState()
	}
	return changed
}

func (a *App) startSession() error {
	a.mu.Lock()
	alreadyStarted := a.started
	if !a.started {
		a.started = true
	}
	a.mu.Unlock()

	if a.client.Store.ID == nil {
		qrChan, err := a.client.GetQRChannel(context.Background())
		if err != nil {
			a.mu.Lock()
			a.started = false
			a.mu.Unlock()
			return err
		}
		go func() {
			for evt := range qrChan {
				if evt.Event == "code" {
					a.broadcast(EventEnvelope{Type: "qr", Payload: evt.Code})
				} else if evt.Event == "timeout" {
					a.broadcast(EventEnvelope{Type: "status", Payload: "QR timed out, retrying..."})
				}
			}
		}()
	}
	if alreadyStarted {
		a.mu.RLock()
		connected := a.connected
		a.mu.RUnlock()
		if connected && a.client.IsConnected() && a.client.IsLoggedIn() {
			return nil
		}
	}
	if err := a.client.Connect(); err != nil {
		a.mu.Lock()
		a.started = false
		a.mu.Unlock()
		return err
	}
	a.recanonicalizeState()
	a.mu.RLock()
	bootstrap := a.needsBootstrapSync
	a.mu.RUnlock()
	if bootstrap {
		go a.bootstrapFromStore()
	}
	go a.refreshGroupMetadata()
	return nil
}

func (a *App) recanonicalizeState() {
	a.mu.Lock()
	a.migrateStateCanonicalIDs(&a.state)
	a.mu.Unlock()
	a.persistState()
	a.broadcast(EventEnvelope{Type: "contacts:updated"})
	a.broadcast(EventEnvelope{Type: "chats:loaded"})
}

func (a *App) refreshGroupMetadata() int {
	if a == nil || a.client == nil || !a.client.IsConnected() || !a.client.IsLoggedIn() {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	groups, err := a.client.GetJoinedGroups(ctx)
	if err != nil {
		return 0
	}

	changed := 0
	a.mu.Lock()
	for _, g := range groups {
		if g == nil || g.JID.IsEmpty() {
			continue
		}
		cid := a.canonicalizeChatID(g.JID.String())
		if cid == "" {
			continue
		}
		ch := a.state.Chats[cid]
		ch.ID = cid
		name := strings.TrimSpace(g.Name)
		if name != "" && ch.Name != name {
			ch.Name = name
			changed++
		}
		if topic := strings.TrimSpace(g.Topic); topic != "" && ch.Subject != topic {
			ch.Subject = topic
			changed++
		}
		if ch.ConversationTimestamp == 0 {
			msgs := a.state.Messages[cid]
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].MessageTimestamp > 0 {
					ch.ConversationTimestamp = msgs[i].MessageTimestamp
					break
				}
			}
		}
		a.state.Chats[cid] = ch
		delete(a.state.Contacts, cid)
	}
	a.mu.Unlock()

	if changed > 0 {
		a.persistState()
		a.broadcast(EventEnvelope{Type: "chats:loaded"})
	}
	return changed
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	connected := a.connected
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "connected": connected})
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true
		}
		return isAllowedOrigin(origin)
	},
}

func (a *App) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.mu.RLock()
	connected := a.connected
	a.mu.RUnlock()
	a.wsMu.Lock()
	if connected {
		if data, err := json.Marshal(EventEnvelope{Type: "ready"}); err == nil {
			_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				a.wsMu.Unlock()
				_ = conn.Close()
				return
			}
		}
		if data, err := json.Marshal(EventEnvelope{Type: "chats:loaded"}); err == nil {
			_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				a.wsMu.Unlock()
				_ = conn.Close()
				return
			}
		}
	}
	a.wsClients[conn] = struct{}{}
	a.wsMu.Unlock()

	go func() {
		defer func() {
			a.wsMu.Lock()
			delete(a.wsClients, conn)
			a.wsMu.Unlock()
			_ = conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

func (a *App) broadcast(evt EventEnvelope) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	a.wsMu.Lock()
	defer a.wsMu.Unlock()
	for c := range a.wsClients {
		_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := c.WriteMessage(websocket.TextMessage, data); err != nil {
			_ = c.Close()
			delete(a.wsClients, c)
		}
	}
}

func (a *App) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := a.startSession(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleChats(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	rawChats := make([]Chat, 0, len(a.state.Chats))
	for _, c := range a.state.Chats {
		rawChats = append(rawChats, c)
	}
	contacts := make(map[string]Contact, len(a.state.Contacts))
	for k, v := range a.state.Contacts {
		contacts[k] = v
	}
	a.mu.RUnlock()

	nameByID := map[string]string{}
	for _, c := range rawChats {
		if n := strings.TrimSpace(c.Name); n != "" {
			nameByID[c.ID] = n
		}
	}
	for id, ct := range contacts {
		if strings.HasSuffix(id, "@g.us") {
			continue
		}
		if n := strings.TrimSpace(ct.Notify); n != "" {
			nameByID[id] = n
		} else if n := strings.TrimSpace(ct.Name); n != "" {
			nameByID[id] = n
		}
	}

	mergedByID := map[string]Chat{}
	for _, c := range rawChats {
		resolvedID := a.canonicalizeChatID(c.ID)
		if resolvedID != "" {
			c.ID = resolvedID
		}
		if c.Name == "" && !strings.HasSuffix(c.ID, "@g.us") {
			if ct, ok := contacts[c.ID]; ok {
				if n := strings.TrimSpace(ct.Notify); n != "" {
					c.Name = n
				} else if n := strings.TrimSpace(ct.Name); n != "" {
					c.Name = n
				}
			}
		}
		if c.Name == "" && a.client != nil && a.client.Store != nil && a.client.Store.LIDs != nil {
			if jid, err := types.ParseJID(c.ID); err == nil && jid.Server == types.DefaultUserServer {
				if lid, err := types.ParseJID(jid.User + "@lid"); err == nil {
					ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
					pn, err := a.client.Store.LIDs.GetPNForLID(ctx, lid)
					cancel()
					if err == nil && pn.User != "" {
						pnID := canonicalChatID(pn.String())
						if ct, ok := contacts[pnID]; ok {
							if n := strings.TrimSpace(ct.Notify); n != "" {
								c.Name = n
							} else if n := strings.TrimSpace(ct.Name); n != "" {
								c.Name = n
							}
						}
						if c.Name == "" {
							if n := strings.TrimSpace(nameByID[pnID]); n != "" {
								c.Name = n
							}
						}
					}
				}
			}
		}
		mergedByID[c.ID] = mergeChat(mergedByID[c.ID], c)
	}

	chats := make([]Chat, 0, len(mergedByID))
	for _, c := range mergedByID {
		chats = append(chats, c)
	}
	sort.Slice(chats, func(i, j int) bool {
		return chats[i].ConversationTimestamp > chats[j].ConversationTimestamp
	})
	writeJSON(w, http.StatusOK, map[string]any{"chats": chats})
}

func (a *App) handleContacts(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	contacts := make([]Contact, 0, len(a.state.Contacts))
	for _, c := range a.state.Contacts {
		contacts = append(contacts, c)
	}
	a.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{"contacts": contacts})
}

func (a *App) handleResolveLIDPN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if a.client == nil || a.client.Store == nil || a.client.Store.LIDs == nil {
		writeErr(w, http.StatusInternalServerError, "lid mapping store unavailable")
		return
	}

	raw := strings.TrimSpace(r.URL.Query().Get("id"))
	if raw == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	jid, err := types.ParseJID(raw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid jid")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	out := map[string]any{
		"input":  raw,
		"server": jid.Server,
	}

	switch jid.Server {
	case types.DefaultUserServer:
		lid, err := a.client.Store.LIDs.GetLIDForPN(ctx, jid)
		out["lookup"] = "pn_to_lid"
		out["lid"] = lid.String()
		if err != nil {
			out["error"] = err.Error()
		}
	case types.HiddenUserServer:
		pn, err := a.client.Store.LIDs.GetPNForLID(ctx, jid)
		out["lookup"] = "lid_to_pn"
		out["pn"] = pn.String()
		if err != nil {
			out["error"] = err.Error()
		}
	default:
		writeErr(w, http.StatusBadRequest, "id must be @s.whatsapp.net or @lid")
		return
	}

	writeJSON(w, http.StatusOK, out)
}

func (a *App) handleSyncContacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if a.client == nil || !a.client.IsConnected() || !a.client.IsLoggedIn() {
		writeErr(w, http.StatusConflict, "not connected")
		return
	}

	if a.client.Store != nil && a.client.Store.AppState != nil {
		appStateCtx, appStateCancel := context.WithTimeout(context.Background(), 10*time.Second)
		a.safeFetchAppState(appStateCtx, appstate.WAPatchCriticalBlock)
		a.safeFetchAppState(appStateCtx, appstate.WAPatchRegularLow)
		a.safeFetchAppState(appStateCtx, appstate.WAPatchRegularHigh)
		a.safeFetchAppState(appStateCtx, appstate.WAPatchRegular)
		appStateCancel()
	}
	if a.client.Store == nil || a.client.Store.Contacts == nil {
		writeErr(w, http.StatusInternalServerError, "contacts store unavailable")
		return
	}

	storeCtx, storeCancel := context.WithTimeout(context.Background(), 12*time.Second)
	allContacts, err := a.client.Store.Contacts.GetAllContacts(storeCtx)
	storeCancel()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	updated := 0
	total := 0
	a.mu.Lock()
	for jid, info := range allContacts {
		raw := strings.TrimSpace(jid.String())
		if raw == "" {
			continue
		}
		total++
		cid := a.canonicalizeChatID(raw)

		name := strings.TrimSpace(info.PushName)
		if name == "" {
			name = strings.TrimSpace(info.FullName)
		}
		if name == "" {
			name = strings.TrimSpace(info.FirstName)
		}
		if name == "" {
			name = strings.TrimSpace(info.BusinessName)
		}

		oldContact := a.state.Contacts[cid]
		contact := oldContact
		contact.ID = cid
		if name != "" {
			contact.Name = name
			contact.Notify = name
		}
		a.state.Contacts[cid] = contact

		oldChat := a.state.Chats[cid]
		chat := oldChat
		chat.ID = cid
		if name != "" {
			chat.Name = name
		}
		a.state.Chats[cid] = chat

		if oldContact != contact || oldChat != chat {
			updated++
		}
	}
	a.mu.Unlock()

	// Second pass: ask WA server for missing profile data for unresolved direct chats.
	unresolved := []types.JID{}
	a.mu.RLock()
	for id, ch := range a.state.Chats {
		if strings.HasSuffix(id, "@g.us") || id == "status@broadcast" {
			continue
		}
		if strings.TrimSpace(ch.Name) != "" {
			continue
		}
		ct := a.state.Contacts[id]
		if strings.TrimSpace(ct.Notify) != "" || strings.TrimSpace(ct.Name) != "" {
			continue
		}
		if jid, err := types.ParseJID(id); err == nil && jid.Server == types.DefaultUserServer {
			unresolved = append(unresolved, jid.ToNonAD())
		}
	}
	a.mu.RUnlock()

	enriched := 0
	queried := 0
	lookupErrors := 0
	if len(unresolved) > 0 {
		seen := map[string]struct{}{}
		unique := make([]types.JID, 0, len(unresolved))
		for _, j := range unresolved {
			key := j.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			unique = append(unique, j)
		}
		queried = len(unique)

		const batchSize = 100
		for i := 0; i < len(unique); i += batchSize {
			end := i + batchSize
			if end > len(unique) {
				end = len(unique)
			}
			batch := unique[i:end]
			lookupCtx, lookupCancel := context.WithTimeout(context.Background(), 8*time.Second)
			infoMap, err := a.client.GetUserInfo(lookupCtx, batch)
			lookupCancel()
			if err != nil {
				lookupErrors++
				continue
			}
			a.mu.Lock()
			for pnJID, info := range infoMap {
				cid := a.canonicalizeChatID(pnJID.String())
				name := ""
				if info.VerifiedName != nil && info.VerifiedName.Details != nil {
					name = strings.TrimSpace(info.VerifiedName.Details.GetVerifiedName())
				}
				if name == "" {
					continue
				}

				ct := a.state.Contacts[cid]
				if ct.ID == "" {
					ct.ID = cid
				}
				changed := false
				if strings.TrimSpace(name) != "" && ct.Notify != name {
					ct.Notify = name
					changed = true
				}
				if strings.TrimSpace(name) != "" && ct.Name != name {
					ct.Name = name
					changed = true
				}
				a.state.Contacts[cid] = ct

				ch := a.state.Chats[cid]
				if ch.ID == "" {
					ch.ID = cid
				}
				if strings.TrimSpace(name) != "" && ch.Name != name {
					ch.Name = name
					changed = true
				}
				a.state.Chats[cid] = ch

				if changed {
					enriched++
				}
			}
			a.mu.Unlock()
		}
	}

	if updated > 0 || enriched > 0 {
		a.recanonicalizeState()
	}
	unresolvedAfter := 0
	a.mu.RLock()
	for id, ch := range a.state.Chats {
		if strings.HasSuffix(id, "@g.us") || id == "status@broadcast" {
			continue
		}
		if strings.TrimSpace(ch.Name) != "" {
			continue
		}
		ct := a.state.Contacts[id]
		if strings.TrimSpace(ct.Notify) != "" || strings.TrimSpace(ct.Name) != "" {
			continue
		}
		unresolvedAfter++
	}
	a.mu.RUnlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"total":        total,
		"updated":      updated,
		"enriched":     enriched,
		"queried":      queried,
		"lookupErrors": lookupErrors,
		"unresolved":   unresolvedAfter,
	})
}

func (a *App) handleSyncGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if a.client == nil || !a.client.IsConnected() || !a.client.IsLoggedIn() {
		writeErr(w, http.StatusConflict, "not connected")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	groups, err := a.client.GetJoinedGroups(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	updated := a.refreshGroupMetadata()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"total":   len(groups),
		"updated": updated,
	})
}

func (a *App) handleMessages(w http.ResponseWriter, r *http.Request) {
	chatID := a.canonicalizeChatID(strings.TrimSpace(r.URL.Query().Get("chatId")))
	if chatID == "" {
		writeErr(w, http.StatusBadRequest, "chatId is required")
		return
	}
	limit := 50
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, maxMessagesResponseLimit)
	}

	a.mu.RLock()
	all := append([]WireMessage(nil), a.state.Messages[chatID]...)
	a.mu.RUnlock()
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": all})
}

func (a *App) requireConnectedClient(w http.ResponseWriter) bool {
	if a == nil || a.client == nil || !a.client.IsConnected() || !a.client.IsLoggedIn() {
		writeErr(w, http.StatusConflict, "not connected")
		return false
	}
	return true
}

func (a *App) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireConnectedClient(w) {
		return
	}
	var req struct {
		ChatID             string `json:"chatId"`
		Text               string `json:"text"`
		ReplyToMsgID       string `json:"replyToMsgId,omitempty"`
		ReplyToText        string `json:"replyToText,omitempty"`
		ReplyToParticipant string `json:"replyToParticipant,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.ChatID = a.canonicalizeChatID(req.ChatID)
	req.Text = strings.TrimSpace(sanitizeOutgoingText(req.Text))
	if req.ChatID == "" || !hasVisibleText(req.Text) {
		writeErr(w, http.StatusBadRequest, "chatId and text are required")
		return
	}

	jid, err := types.ParseJID(req.ChatID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid chatId")
		return
	}

	var msg *waProto.Message
	if req.ReplyToMsgID != "" {
		// build quoted message stub for ContextInfo
		quotedMsg := &waProto.Message{Conversation: proto.String(req.ReplyToText)}
		participant := req.ReplyToParticipant
		if participant == "" {
			participant = req.ChatID
		}
		msg = &waProto.Message{
			ExtendedTextMessage: &waProto.ExtendedTextMessage{
				Text: proto.String(req.Text),
				ContextInfo: &waProto.ContextInfo{
					StanzaID:      proto.String(req.ReplyToMsgID),
					Participant:   proto.String(participant),
					QuotedMessage: quotedMsg,
				},
			},
		}
	} else {
		msg = &waProto.Message{Conversation: proto.String(req.Text)}
	}

	resp, err := a.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().Unix()
	wireMsg := map[string]any{"conversation": req.Text}
	if req.ReplyToMsgID != "" {
		wireMsg = map[string]any{
			"extendedTextMessage": map[string]any{
				"text":              req.Text,
				"quotedText":        req.ReplyToText,
				"quotedParticipant": req.ReplyToParticipant,
			},
		}
	}
	wire := WireMessage{
		Key: WireKey{
			ID:        resp.ID,
			RemoteJID: req.ChatID,
			FromMe:    true,
		},
		Message:          wireMsg,
		MessageTimestamp: now,
		ReceiptStatus:    "sent",
	}
	a.upsertMessage(req.ChatID, wire)
	writeJSON(w, http.StatusOK, map[string]any{"message": wire})
}

func (a *App) handleSendFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !a.requireConnectedClient(w) {
		return
	}
	var req struct {
		ChatID  string `json:"chatId"`
		Path    string `json:"path"`
		Caption string `json:"caption"`
		Kind    string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.ChatID = a.canonicalizeChatID(strings.TrimSpace(req.ChatID))
	req.Path = filepath.Clean(strings.TrimSpace(req.Path))
	req.Caption = strings.TrimSpace(sanitizeOutgoingText(req.Caption))
	req.Kind = strings.ToLower(strings.TrimSpace(req.Kind))
	if req.ChatID == "" || req.Path == "" {
		writeErr(w, http.StatusBadRequest, "chatId and path are required")
		return
	}

	jid, err := types.ParseJID(req.ChatID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid chatId")
		return
	}

	info, err := os.Stat(req.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "file not found")
		return
	}
	if info.IsDir() {
		writeErr(w, http.StatusBadRequest, "path must be a file")
		return
	}
	if !info.Mode().IsRegular() {
		writeErr(w, http.StatusBadRequest, "path must be a regular file")
		return
	}
	if info.Size() > maxUploadBytes {
		writeErr(w, http.StatusBadRequest, "file too large: max 100 MB")
		return
	}
	if err := validateSendFileInput(req.Path, req.Kind); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	mediaType, err := mediaTypeForSendKind(req.Kind)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	data, err := os.ReadFile(req.Path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	upload, err := a.client.Upload(context.Background(), data, mediaType)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	msg, wireBody, err := buildOutgoingMediaMessage(req.Kind, req.Path, req.Caption, upload, data)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := a.client.SendMessage(context.Background(), jid, msg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	var mediaProto string
	if b, err := proto.Marshal(msg); err == nil {
		mediaProto = base64.StdEncoding.EncodeToString(b)
	}
	wire := WireMessage{
		Key: WireKey{
			ID:        resp.ID,
			RemoteJID: req.ChatID,
			FromMe:    true,
		},
		Message:          wireBody,
		MessageTimestamp: time.Now().Unix(),
		MediaProto:       mediaProto,
		ReceiptStatus:    "sent",
	}
	a.upsertMessage(req.ChatID, wire)
	writeJSON(w, http.StatusOK, map[string]any{"message": wire})
}

func (a *App) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		ChatID string `json:"chatId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	req.ChatID = strings.TrimSpace(req.ChatID)
	req.ChatID = a.canonicalizeChatID(req.ChatID)
	if req.ChatID == "" {
		writeErr(w, http.StatusBadRequest, "chatId is required")
		return
	}

	a.mu.Lock()
	chat := a.state.Chats[req.ChatID]
	chat.ID = req.ChatID
	chat.UnreadCount = 0
	a.state.Chats[req.ChatID] = chat
	// gather unread messages to actually tell WhatsApp server we read them
	msgs := a.state.Messages[req.ChatID]
	senderToIDs := make(map[string][]types.MessageID)
	for _, msg := range msgs {
		if !msg.Key.FromMe {
			sender := msg.Key.Participant
			if sender == "" {
				sender = msg.Key.RemoteJID
			}
			senderToIDs[sender] = append(senderToIDs[sender], types.MessageID(msg.Key.ID))
		}
	}
	a.mu.Unlock()

	// actually inform WhatsApp server!
	if a.client != nil {
		chatJID, _ := types.ParseJID(req.ChatID)
		for senderStr, ids := range senderToIDs {
			senderJID, _ := types.ParseJID(senderStr)
			_ = a.client.MarkRead(context.Background(), ids, time.Now(), chatJID, senderJID)
		}
	}

	a.persistState()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleProfilePicture(w http.ResponseWriter, r *http.Request) {
	if !a.requireConnectedClient(w) {
		return
	}
	jidRaw := strings.TrimSpace(r.URL.Query().Get("jid"))
	if jidRaw == "" {
		writeErr(w, http.StatusBadRequest, "jid is required")
		return
	}
	jid, err := types.ParseJID(jidRaw)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid jid")
		return
	}
	info, err := a.client.GetProfilePictureInfo(context.Background(), jid, &whatsmeow.GetProfilePictureParams{})
	if err != nil || info == nil {
		writeJSON(w, http.StatusOK, map[string]any{"url": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"url": info.URL})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	a.mu.Lock()
	a.shuttingDown = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.shuttingDown = false
		a.mu.Unlock()
	}()
	hadRuntimeResources := a.client != nil || a.storeContainer != nil
	var warnings []string
	if a.client != nil {
		if a.client.Store != nil && a.client.Store.ID != nil {
			if err := a.client.Logout(context.Background()); err != nil {
				warnings = append(warnings, fmt.Sprintf("remote logout failed: %v", err))
			}
		}
		a.client.Disconnect()
		if a.client.Store != nil && a.client.Store.ID != nil {
			if err := a.client.Store.Delete(context.Background()); err != nil {
				writeErr(w, http.StatusInternalServerError, fmt.Sprintf("store delete failed: %v", err))
				return
			} else {
				a.client.Store.ID = nil
			}
		}
	}
	a.mu.Lock()
	a.started = false
	a.connected = false
	a.needsBootstrapSync = false
	a.state = PersistedState{
		Chats:    map[string]Chat{},
		Contacts: map[string]Contact{},
		Messages: map[string][]WireMessage{},
	}
	a.mu.Unlock()
	// Close all DB connections before deleting files (required on Windows to release file locks).
	if a.storeContainer != nil {
		_ = a.storeContainer.Close()
		a.storeContainer = nil
	}
	if a.db != nil {
		_ = a.db.Close()
		a.db = nil
	}
	if strings.TrimSpace(a.cacheDir) != "" {
		if hadRuntimeResources {
			if err := a.resetPersistentStorage(); err != nil {
				writeErr(w, http.StatusInternalServerError, fmt.Sprintf("state cleanup failed: %v", err))
				return
			}
		} else {
			if err := os.RemoveAll(a.cacheDir); err != nil {
				writeErr(w, http.StatusInternalServerError, fmt.Sprintf("state cleanup failed: %v", err))
				return
			}
			if err := os.MkdirAll(a.cacheDir, 0o755); err != nil {
				writeErr(w, http.StatusInternalServerError, fmt.Sprintf("state cleanup failed: %v", err))
				return
			}
			a.cachePath = filepath.Join(a.cacheDir, "state.json")
		}
		if err := a.persistStateWithErr(); err != nil {
			writeErr(w, http.StatusInternalServerError, fmt.Sprintf("state cleanup failed: %v", err))
			return
		}
	} else if err := a.persistStateWithErr(); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("state cleanup failed: %v", err))
		return
	}
	msg := "Logged out successfully"
	if len(warnings) > 0 {
		msg = "Local data cleared; QR login required"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (a *App) toWireMessage(evt *events.Message) WireMessage {
	info := evt.Info
	chatID := info.Chat.String()
	effective := effectiveMessage(evt.Message)
	msg, mediaProto := a.wireMessagePayload(evt.Message, effective, chatID, info.IsGroup)

	key := WireKey{
		ID:        info.ID,
		RemoteJID: chatID,
		FromMe:    info.IsFromMe,
	}
	if info.IsGroup {
		key.Participant = info.Sender.String()
	}

	return WireMessage{
		Key:              key,
		Message:          msg,
		MessageTimestamp: info.Timestamp.Unix(),
		PushName:         evt.Info.PushName,
		MediaProto:       mediaProto,
	}
}

func (a *App) toWireMessageFromHistory(webMsg *waWeb.WebMessageInfo) WireMessage {
	if webMsg == nil {
		return WireMessage{}
	}
	key := webMsg.GetKey()
	m := effectiveMessage(webMsg.GetMessage())
	chatID := key.GetRemoteJID()
	out, mediaProto := a.wireMessagePayload(webMsg.GetMessage(), m, chatID, strings.HasSuffix(chatID, "@g.us"))
	wireKey := WireKey{
		ID:          key.GetID(),
		RemoteJID:   key.GetRemoteJID(),
		FromMe:      key.GetFromMe(),
		Participant: key.GetParticipant(),
	}

	return WireMessage{
		Key:              wireKey,
		Message:          out,
		MessageTimestamp: int64(webMsg.GetMessageTimestamp()),
		PushName:         webMsg.GetPushName(),
		MediaProto:       mediaProto,
	}
}

func normalizeQuotedParticipant(participant, selfID string, canonicalize func(string) string) string {
	phoneIdentity := func(jid string) string {
		local := phoneFromJID(jid)
		local = strings.SplitN(local, ":", 2)[0]
		return strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, local)
	}
	participant = strings.TrimSpace(participant)
	if participant == "" {
		return ""
	}
	if canonicalize != nil {
		participant = canonicalize(participant)
	}
	if strings.TrimSpace(selfID) != "" {
		selfCanonical := selfID
		if canonicalize != nil {
			selfCanonical = canonicalize(selfID)
		}
		if participant == selfCanonical || phoneIdentity(participant) == phoneIdentity(selfCanonical) {
			return ""
		}
	}
	return participant
}

func phoneIdentity(jid string) string {
	local := phoneFromJID(jid)
	local = strings.SplitN(local, ":", 2)[0]
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, local)
}

func quotedFromMeForChat(chatID, quotedParticipant string, isGroup bool) bool {
	quotedParticipant = strings.TrimSpace(quotedParticipant)
	if quotedParticipant == "" {
		return true
	}
	if isGroup {
		return false
	}
	return phoneIdentity(quotedParticipant) != "" && phoneIdentity(quotedParticipant) != phoneIdentity(chatID)
}

func (a *App) wireMessagePayload(raw, effective *waProto.Message, chatID string, isGroup bool) (map[string]any, string) {
	msg := map[string]any{}
	var mediaProto string
	if txt := effective.GetConversation(); txt != "" {
		msg["conversation"] = txt
	}
	if ext := effective.GetExtendedTextMessage(); ext != nil {
		entry := map[string]any{"text": ext.GetText()}
		if ctx := ext.GetContextInfo(); ctx != nil && ctx.GetQuotedMessage() != nil {
			entry["quotedText"] = quotedText(ctx.GetQuotedMessage())
			selfID := ""
			if a != nil && a.client != nil && a.client.Store != nil && a.client.Store.ID != nil {
				selfID = a.client.Store.ID.String()
			}
			entry["quotedParticipant"] = normalizeQuotedParticipant(ctx.GetParticipant(), selfID, func(id string) string {
				if a == nil {
					return strings.TrimSpace(id)
				}
				return a.canonicalizeChatID(id)
			})
			entry["quotedFromMe"] = quotedFromMeForChat(chatID, fmt.Sprint(entry["quotedParticipant"]), isGroup)
		}
		msg["extendedTextMessage"] = entry
	}
	if img := effective.GetImageMessage(); img != nil {
		msg["imageMessage"] = map[string]any{"caption": img.GetCaption(), "mimetype": img.GetMimetype()}
	}
	if vid := effective.GetVideoMessage(); vid != nil {
		msg["videoMessage"] = map[string]any{"caption": vid.GetCaption(), "mimetype": vid.GetMimetype()}
	}
	if doc := effective.GetDocumentMessage(); doc != nil {
		msg["documentMessage"] = map[string]any{"caption": doc.GetCaption(), "fileName": doc.GetFileName(), "mimetype": doc.GetMimetype()}
	}
	if aud := effective.GetAudioMessage(); aud != nil {
		msg["audioMessage"] = map[string]any{"ptt": aud.GetPTT(), "mimetype": aud.GetMimetype()}
	}
	if stk := effective.GetStickerMessage(); stk != nil {
		msg["stickerMessage"] = map[string]any{"mimetype": stk.GetMimetype()}
	}
	if rxn := effective.GetReactionMessage(); rxn != nil {
		msg["reactionMessage"] = map[string]any{
			"emoji":       rxn.GetText(),
			"targetMsgID": rxn.GetKey().GetID(),
		}
	}
	if protocol := protocolMessagePayload(raw, effective); protocol != nil {
		msg["protocolMessage"] = protocol
	}
	if len(msg) == 0 {
		msg["unknown"] = map[string]any{
			"rawFields":       messageFieldNames(raw),
			"effectiveFields": messageFieldNames(effective),
		}
	}
	isMedia := effective.GetImageMessage() != nil || effective.GetVideoMessage() != nil ||
		effective.GetDocumentMessage() != nil || effective.GetAudioMessage() != nil ||
		effective.GetStickerMessage() != nil
	if isMedia {
		if b, err := proto.Marshal(effective); err == nil {
			mediaProto = base64.StdEncoding.EncodeToString(b)
		}
	}
	return msg, mediaProto
}

func protocolMessagePayload(raw, effective *waProto.Message) map[string]any {
	var protocol *waProto.ProtocolMessage
	switch {
	case effective != nil && effective.GetProtocolMessage() != nil:
		protocol = effective.GetProtocolMessage()
	case raw != nil && raw.GetProtocolMessage() != nil:
		protocol = raw.GetProtocolMessage()
	default:
		return nil
	}
	out := map[string]any{
		"type": protocol.GetType().String(),
	}
	if key := protocol.GetKey(); key != nil {
		if id := key.GetID(); id != "" {
			out["targetMsgID"] = id
		}
	}
	if timer := protocol.GetEphemeralExpiration(); timer > 0 {
		out["ephemeralExpiration"] = timer
	}
	if edited := protocol.GetEditedMessage(); edited != nil {
		if text := quotedText(edited); text != "" {
			out["editedText"] = text
		}
	}
	return out
}

func effectiveMessage(msg *waProto.Message) *waProto.Message {
	for msg != nil {
		switch {
		case msg.GetDeviceSentMessage() != nil:
			msg = msg.GetDeviceSentMessage().GetMessage()
		case msg.GetCommentMessage() != nil:
			msg = msg.GetCommentMessage().GetMessage()
		case msg.GetEphemeralMessage() != nil:
			msg = msg.GetEphemeralMessage().GetMessage()
		case msg.GetViewOnceMessage() != nil:
			msg = msg.GetViewOnceMessage().GetMessage()
		case msg.GetViewOnceMessageV2() != nil:
			msg = msg.GetViewOnceMessageV2().GetMessage()
		case msg.GetViewOnceMessageV2Extension() != nil:
			msg = msg.GetViewOnceMessageV2Extension().GetMessage()
		case msg.GetDocumentWithCaptionMessage() != nil:
			msg = msg.GetDocumentWithCaptionMessage().GetMessage()
		case msg.GetEditedMessage() != nil:
			msg = msg.GetEditedMessage().GetMessage()
		default:
			return msg
		}
	}
	return nil
}

func messageFieldNames(msg *waProto.Message) []string {
	if msg == nil {
		return nil
	}
	fields := make([]string, 0, 4)
	msg.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		name := string(fd.Name())
		if name != "messageContextInfo" {
			fields = append(fields, name)
		}
		return true
	})
	sort.Strings(fields)
	return fields
}

func dedupeKey(m WireMessage) string {
	if m.Key.ID != "" {
		return "id:" + m.Key.ID
	}
	body := ""
	if s, ok := m.Message["conversation"].(string); ok {
		body = s
	}
	if body == "" {
		if ext, ok := m.Message["extendedTextMessage"].(map[string]any); ok {
			if txt, ok := ext["text"].(string); ok {
				body = txt
			}
		}
	}
	if len(body) > 64 {
		body = body[:64]
	}
	fromMe := "0"
	if m.Key.FromMe {
		fromMe = "1"
	}
	return m.Key.RemoteJID + "|" + m.Key.Participant + "|" + fromMe + "|" + strconv.FormatInt(m.MessageTimestamp, 10) + "|" + body
}

func (a *App) loadState() {
	raw, err := os.ReadFile(a.cachePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			a.mu.Lock()
			a.needsBootstrapSync = true
			a.mu.Unlock()
			return
		}
		log.Printf("state read error: %v", err)
		return
	}
	if strings.TrimSpace(string(raw)) == "" {
		a.mu.Lock()
		a.needsBootstrapSync = true
		a.mu.Unlock()
		return
	}
	var state PersistedState
	if err := json.Unmarshal(raw, &state); err != nil {
		log.Printf("state parse error: %v", err)
		a.mu.Lock()
		a.state = PersistedState{
			Chats:    map[string]Chat{},
			Contacts: map[string]Contact{},
			Messages: map[string][]WireMessage{},
		}
		a.needsBootstrapSync = true
		a.mu.Unlock()
		if removeErr := os.Remove(a.cachePath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Printf("state cleanup error: %v", removeErr)
		}
		return
	}
	if state.Chats == nil {
		state.Chats = map[string]Chat{}
	}
	if state.Contacts == nil {
		state.Contacts = map[string]Contact{}
	}
	if state.Messages == nil {
		state.Messages = map[string][]WireMessage{}
	}
	a.migrateStateCanonicalIDs(&state)
	reconciled := reconcileChatTimestamps(&state)
	a.mu.Lock()
	a.state = state
	a.needsBootstrapSync = false
	a.mu.Unlock()
	if reconciled {
		a.persistState()
	}
}

// seeding chats from store so UI isn't empty on first start
func (a *App) bootstrapFromStore() {
	a.mu.Lock()
	if !a.needsBootstrapSync {
		a.mu.Unlock()
		return
	}
	a.needsBootstrapSync = false
	a.mu.Unlock()

	a.broadcast(EventEnvelope{Type: "status", Payload: "Bootstrapping chat state..."})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	if a.client == nil || a.client.Store == nil {
		return
	}
	if a.client.Store.AppState == nil {
		log.Printf("bootstrap: app state store unavailable, skipping app state sync")
	} else {
		a.safeFetchAppState(ctx, appstate.WAPatchCriticalBlock)
		a.safeFetchAppState(ctx, appstate.WAPatchRegularLow)
		a.safeFetchAppState(ctx, appstate.WAPatchRegularHigh)
		a.safeFetchAppState(ctx, appstate.WAPatchRegular)
	}

	if a.client.Store.Contacts == nil {
		return
	}
	allContacts, err := a.client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return
	}

	a.mu.Lock()
	seeded := 0
	for jid, info := range allContacts {
		raw := strings.TrimSpace(jid.String())
		if raw == "" {
			continue
		}
		cid := a.canonicalizeChatID(raw)
		name := strings.TrimSpace(info.PushName)
		if name == "" {
			name = strings.TrimSpace(info.FullName)
		}
		if name == "" {
			name = strings.TrimSpace(info.FirstName)
		}
		if name == "" {
			name = strings.TrimSpace(info.BusinessName)
		}

		ct := a.state.Contacts[cid]
		ct.ID = cid
		if name != "" && ct.Notify == "" {
			ct.Notify = name
		}
		a.state.Contacts[cid] = ct

		ch := a.state.Chats[cid]
		ch.ID = cid
		if name != "" && ch.Name == "" {
			ch.Name = name
		}
		a.state.Chats[cid] = ch
		seeded++
	}
	a.mu.Unlock()

	if seeded > 0 {
		a.persistState()
		a.broadcast(EventEnvelope{Type: "contacts:updated"})
		a.broadcast(EventEnvelope{Type: "chats:loaded"})
		a.broadcast(EventEnvelope{Type: "status", Payload: "Bootstrap complete"})
	}
}

func (a *App) safeFetchAppState(ctx context.Context, name appstate.WAPatchName) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("bootstrap: recovered panic during app state fetch %s: %v", name, r)
		}
	}()
	if err := a.client.FetchAppState(ctx, name, true, false); err != nil {
		log.Printf("bootstrap: failed to fetch app state %s: %v", name, err)
	}
}

func (a *App) persistState() {
	if a == nil {
		return
	}
	a.mu.RLock()
	shuttingDown := a.shuttingDown
	a.mu.RUnlock()
	if shuttingDown {
		return
	}
	_ = a.persistStateWithErr()
}

func (a *App) persistStateWithErr() error {
	a.mu.RLock()
	b, err := json.Marshal(a.state)
	a.mu.RUnlock()
	if err != nil {
		return err
	}
	return writeFileAtomic(a.cachePath, b, 0o644)
}

// Atomic write for persisted state files.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if runtime.GOOS != "windows" {
			return err
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		if err := os.Rename(tmpPath, path); err != nil {
			return err
		}
	}
	if runtime.GOOS != "windows" {
		if dirHandle, err := os.Open(dir); err == nil {
			_ = dirHandle.Sync()
			_ = dirHandle.Close()
		}
	}
	return nil
}

func reconcileChatTimestamps(state *PersistedState) bool {
	if state == nil {
		return false
	}
	changed := false
	for chatID, msgs := range state.Messages {
		var latest int64
		for _, msg := range msgs {
			if msg.MessageTimestamp > latest {
				latest = msg.MessageTimestamp
			}
		}
		chat := state.Chats[chatID]
		if chat.ID == "" {
			chat.ID = chatID
		}
		if latest > chat.ConversationTimestamp {
			chat.ConversationTimestamp = latest
			changed = true
		}
		state.Chats[chatID] = chat
	}
	return changed
}

func phoneFromJID(jid string) string {
	p := strings.Split(jid, "@")
	return p[0]
}

func canonicalChatID(chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	if strings.HasSuffix(chatID, "@g.us") || chatID == "status@broadcast" {
		return chatID
	}
	local := phoneFromJID(chatID)
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, local)
	if len(digits) >= 6 {
		return digits + "@s.whatsapp.net"
	}
	return chatID
}

func (a *App) canonicalizeChatID(chatID string) string {
	base := canonicalChatID(chatID)
	if base == "" || strings.HasSuffix(base, "@g.us") || base == "status@broadcast" {
		return base
	}
	if a == nil || a.client == nil || a.client.Store == nil || a.client.Store.LIDs == nil {
		return base
	}

	jid, err := types.ParseJID(strings.TrimSpace(chatID))
	if err != nil {
		jid, err = types.ParseJID(base)
		if err != nil {
			return base
		}
	}
	jid = jid.ToNonAD()

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	switch jid.Server {
	case types.HiddenUserServer:
		if pn, err := a.client.Store.LIDs.GetPNForLID(ctx, jid); err == nil && pn.User != "" {
			return canonicalChatID(pn.String())
		}
	case types.DefaultUserServer:
		// fixing old cache keys that used LID digits for @s.whatsapp.net
		if possibleLID, err := types.ParseJID(jid.User + "@lid"); err == nil {
			if pn, err := a.client.Store.LIDs.GetPNForLID(ctx, possibleLID); err == nil && pn.User != "" {
				return canonicalChatID(pn.String())
			}
		}
	}

	return base
}

func (a *App) historyConversationChatID(conv *waHistorySync.Conversation) string {
	if conv == nil {
		return ""
	}
	candidates := []string{
		conv.GetPnJID(),
		conv.GetID(),
		conv.GetLidJID(),
		conv.GetNewJID(),
		conv.GetOldJID(),
	}
	for _, raw := range candidates {
		cid := a.canonicalizeChatID(strings.TrimSpace(raw))
		if cid != "" {
			return cid
		}
	}
	return ""
}

func mergeChat(dst Chat, src Chat) Chat {
	if dst.ID == "" {
		dst.ID = src.ID
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.Subject == "" {
		dst.Subject = src.Subject
	}
	if src.ConversationTimestamp > dst.ConversationTimestamp {
		dst.ConversationTimestamp = src.ConversationTimestamp
	}
	if src.UnreadCount > dst.UnreadCount {
		dst.UnreadCount = src.UnreadCount
	}
	return dst
}

func mergeContact(dst Contact, src Contact) Contact {
	if dst.ID == "" {
		dst.ID = src.ID
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	if dst.Notify == "" {
		dst.Notify = src.Notify
	}
	return dst
}

func (a *App) migrateStateCanonicalIDs(state *PersistedState) {
	newChats := map[string]Chat{}
	for rawID, ch := range state.Chats {
		cid := a.canonicalizeChatID(rawID)
		ch.ID = cid
		merged := mergeChat(newChats[cid], ch)
		newChats[cid] = merged
	}

	newContacts := map[string]Contact{}
	for rawID, ct := range state.Contacts {
		cid := a.canonicalizeChatID(rawID)
		ct.ID = cid
		merged := mergeContact(newContacts[cid], ct)
		newContacts[cid] = merged
	}

	newMessages := map[string][]WireMessage{}
	for rawID, msgs := range state.Messages {
		cid := a.canonicalizeChatID(rawID)
		for _, m := range msgs {
			m.Key.RemoteJID = a.canonicalizeChatID(m.Key.RemoteJID)
			if m.Key.RemoteJID == "" {
				m.Key.RemoteJID = cid
			}
			if m.Key.Participant != "" {
				m.Key.Participant = a.canonicalizeChatID(m.Key.Participant)
			}
			newMessages[cid] = append(newMessages[cid], m)
		}
		sort.Slice(newMessages[cid], func(i, j int) bool {
			return newMessages[cid][i].MessageTimestamp < newMessages[cid][j].MessageTimestamp
		})
		if len(newMessages[cid]) > 200 {
			newMessages[cid] = newMessages[cid][len(newMessages[cid])-200:]
		}
	}

	state.Chats = newChats
	state.Contacts = newContacts
	state.Messages = newMessages
}

func (a *App) upsertPermission(phone, name string) {
	if phone == "" {
		return
	}
	if a == nil {
		return
	}
	a.mu.RLock()
	shuttingDown := a.shuttingDown
	db := a.db
	a.mu.RUnlock()
	if shuttingDown || db == nil {
		return
	}
	_, _ = db.Exec(
		`INSERT OR IGNORE INTO chat_permissions (phone, name, allowed) VALUES (?, ?, 0)`,
		phone, name,
	)
}

func (a *App) handleGetWhitelist(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(`SELECT phone, name, allowed FROM chat_permissions ORDER BY phone`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	type entry struct {
		Phone   string `json:"phone"`
		Name    string `json:"name"`
		Allowed int    `json:"allowed"`
	}
	result := []entry{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.Phone, &e.Name, &e.Allowed); err == nil {
			result = append(result, e)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": result})
}

func (a *App) handleSetWhitelist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Phone   string `json:"phone"`
		Name    string `json:"name"`
		Allowed int    `json:"allowed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "phone is required")
		return
	}
	_, err := a.db.Exec(
		`INSERT OR REPLACE INTO chat_permissions (phone, name, allowed) VALUES (?, ?, ?)`,
		req.Phone, req.Name, req.Allowed,
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleSetName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Phone string `json:"phone"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Phone == "" {
		writeErr(w, http.StatusBadRequest, "phone is required")
		return
	}
	// create row if not exists (allowed stays 0), then only update name
	_, err := a.db.Exec(
		`INSERT INTO chat_permissions (phone, name, allowed) VALUES (?, ?, 0)
		 ON CONFLICT(phone) DO UPDATE SET name=excluded.name`,
		req.Phone, req.Name,
	)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *App) handleMediaDownload(w http.ResponseWriter, r *http.Request) {
	if !a.requireConnectedClient(w) {
		return
	}
	chatID := r.URL.Query().Get("chatId")
	msgID := r.URL.Query().Get("msgId")
	if chatID == "" || msgID == "" {
		writeErr(w, http.StatusBadRequest, "chatId and msgId required")
		return
	}
	a.mu.RLock()
	msgs := a.state.Messages[chatID]
	a.mu.RUnlock()
	var target *WireMessage
	for i := range msgs {
		if msgs[i].Key.ID == msgID {
			target = &msgs[i]
			break
		}
	}
	if target == nil || target.MediaProto == "" {
		writeErr(w, http.StatusNotFound, "media not found")
		return
	}
	b, err := base64.StdEncoding.DecodeString(target.MediaProto)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "decode error")
		return
	}
	var msg waProto.Message
	if err := proto.Unmarshal(b, &msg); err != nil {
		writeErr(w, http.StatusInternalServerError, "unmarshal error")
		return
	}
	data, err := a.client.DownloadAny(context.Background(), &msg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ext := mediaExtension(&msg)
	tmp, err := os.CreateTemp("", "whatzap-*"+ext)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "temp file error")
		return
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		writeErr(w, http.StatusInternalServerError, "temp file write error")
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		writeErr(w, http.StatusInternalServerError, "temp file close error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": tmp.Name()})
}

func mediaExtension(msg *waProto.Message) string {
	msg = effectiveMessage(msg)
	if msg == nil {
		return ".bin"
	}
	if img := msg.GetImageMessage(); img != nil {
		if strings.Contains(img.GetMimetype(), "jpeg") {
			return ".jpg"
		}
		return ".png"
	}
	if msg.GetVideoMessage() != nil {
		return ".mp4"
	}
	if doc := msg.GetDocumentMessage(); doc != nil {
		if ext := filepath.Ext(doc.GetFileName()); ext != "" {
			return ext
		}
		return ".bin"
	}
	if msg.GetAudioMessage() != nil {
		return ".ogg"
	}
	if msg.GetStickerMessage() != nil {
		return ".webp"
	}
	return ".bin"
}

func mediaTypeForSendKind(kind string) (whatsmeow.MediaType, error) {
	switch kind {
	case "image":
		return whatsmeow.MediaImage, nil
	case "video":
		return whatsmeow.MediaVideo, nil
	case "document":
		return whatsmeow.MediaDocument, nil
	default:
		return "", fmt.Errorf("unsupported media kind: %s", kind)
	}
}

func validateSendFileInput(path, kind string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if kind == "" {
		return fmt.Errorf("kind is required")
	}
	if kind == "document" {
		return nil
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file")
	}
	defer f.Close()

	header := make([]byte, 512)
	n, err := f.Read(header)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to inspect file")
	}

	sniffedType := http.DetectContentType(header[:n])
	extType := ""
	if ext := strings.ToLower(filepath.Ext(path)); ext != "" {
		extType = mime.TypeByExtension(ext)
	}

	switch kind {
	case "image":
		if isExpectedMediaType(sniffedType, "image/") || isExpectedMediaType(extType, "image/") {
			return nil
		}
		return fmt.Errorf("kind=image but file does not appear to be an image")
	case "video":
		if isExpectedMediaType(sniffedType, "video/") || isExpectedMediaType(extType, "video/") {
			return nil
		}
		return fmt.Errorf("kind=video but file does not appear to be a video")
	default:
		return fmt.Errorf("unsupported media kind: %s", kind)
	}
}

func isExpectedMediaType(actual, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(actual)), prefix)
}

func detectMIMEType(path string, data []byte, fallback string) string {
	if ext := strings.ToLower(filepath.Ext(path)); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return fallback
}

func buildOutgoingMediaMessage(kind, path, caption string, upload whatsmeow.UploadResponse, data []byte) (*waProto.Message, map[string]any, error) {
	mimeType := detectMIMEType(path, data, "application/octet-stream")
	fileName := filepath.Base(path)
	switch kind {
	case "image":
		msg := &waProto.Message{
			ImageMessage: &waProto.ImageMessage{
				Caption:       proto.String(caption),
				Mimetype:      proto.String(mimeType),
				URL:           proto.String(upload.URL),
				DirectPath:    proto.String(upload.DirectPath),
				MediaKey:      upload.MediaKey,
				FileEncSHA256: upload.FileEncSHA256,
				FileSHA256:    upload.FileSHA256,
				FileLength:    proto.Uint64(upload.FileLength),
			},
		}
		return msg, map[string]any{
			"imageMessage": map[string]any{
				"fileName": fileName,
				"caption":  caption,
				"mimetype": mimeType,
			},
		}, nil
	case "video":
		msg := &waProto.Message{
			VideoMessage: &waProto.VideoMessage{
				Caption:       proto.String(caption),
				Mimetype:      proto.String(mimeType),
				URL:           proto.String(upload.URL),
				DirectPath:    proto.String(upload.DirectPath),
				MediaKey:      upload.MediaKey,
				FileEncSHA256: upload.FileEncSHA256,
				FileSHA256:    upload.FileSHA256,
				FileLength:    proto.Uint64(upload.FileLength),
			},
		}
		return msg, map[string]any{
			"videoMessage": map[string]any{
				"fileName": fileName,
				"caption":  caption,
				"mimetype": mimeType,
			},
		}, nil
	case "document":
		msg := &waProto.Message{
			DocumentMessage: &waProto.DocumentMessage{
				Caption:       proto.String(caption),
				Title:         proto.String(fileName),
				FileName:      proto.String(fileName),
				Mimetype:      proto.String(mimeType),
				URL:           proto.String(upload.URL),
				DirectPath:    proto.String(upload.DirectPath),
				MediaKey:      upload.MediaKey,
				FileEncSHA256: upload.FileEncSHA256,
				FileSHA256:    upload.FileSHA256,
				FileLength:    proto.Uint64(upload.FileLength),
			},
		}
		return msg, map[string]any{
			"documentMessage": map[string]any{
				"caption":  caption,
				"fileName": fileName,
				"mimetype": mimeType,
			},
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported media kind: %s", kind)
	}
}

func quotedText(m *waProto.Message) string {
	m = effectiveMessage(m)
	if m == nil {
		return ""
	}
	if t := m.GetConversation(); t != "" {
		return t
	}
	if ext := m.GetExtendedTextMessage(); ext != nil {
		return ext.GetText()
	}
	if m.GetImageMessage() != nil {
		return "[image]"
	}
	if m.GetVideoMessage() != nil {
		return "[video]"
	}
	if doc := m.GetDocumentMessage(); doc != nil {
		if doc.GetFileName() != "" {
			return "[file: " + doc.GetFileName() + "]"
		}
		return "[document]"
	}
	if aud := m.GetAudioMessage(); aud != nil {
		if aud.GetPTT() {
			return "[voice]"
		}
		return "[audio]"
	}
	if m.GetStickerMessage() != nil {
		return "[sticker]"
	}
	return "[message]"
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		w.Header().Add("Vary", "Origin")
		if isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "authorization,content-type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			if origin != "" && !isAllowedOrigin(origin) {
				writeErr(w, http.StatusForbidden, "origin not allowed")
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
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
