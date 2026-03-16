package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Config path check.
func TestSaveAndLoadConfigUsesOverride(t *testing.T) {
	t.Setenv("WHATZAP_DATA_DIR", t.TempDir())

	currentConfig = Config{
		ThemeName:    "aurora",
		MouseEnabled: false,
		SoundEnabled: false,
		SoundProfile: 5,
	}
	saveConfig()

	currentConfig = Config{}
	loadConfig()

	if currentConfig.ThemeName != "aurora" {
		t.Fatalf("theme name = %q, want aurora", currentConfig.ThemeName)
	}
	if currentConfig.MouseEnabled {
		t.Fatalf("mouse enabled = true, want false")
	}
	if currentConfig.SoundEnabled {
		t.Fatalf("sound enabled = true, want false")
	}
	if currentConfig.SoundProfile != 5 {
		t.Fatalf("sound profile = %d, want 5", currentConfig.SoundProfile)
	}
}

func TestRenderMessageBodyProtocolMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  map[string]any
		want string
	}{
		{
			name: "revoke",
			msg: map[string]any{
				"protocolMessage": map[string]any{"type": "REVOKE"},
			},
			want: "[message deleted]",
		},
		{
			name: "edit",
			msg: map[string]any{
				"protocolMessage": map[string]any{"type": "MESSAGE_EDIT", "editedText": "fixed text"},
			},
			want: "[message edited] fixed text",
		},
		{
			name: "ephemeral setting",
			msg: map[string]any{
				"protocolMessage": map[string]any{"type": "EPHEMERAL_SETTING", "ephemeralExpiration": float64(604800)},
			},
			want: "[disappearing messages] 7d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderMessageBody(tt.msg); got != tt.want {
				t.Fatalf("renderMessageBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWrapTextWithPrefixReservesFirstLineWidth(t *testing.T) {
	got := wrapTextWithPrefix("alpha beta gamma delta", 13, 8)
	want := "alpha\nbeta gamma\ndelta"
	if got != want {
		t.Fatalf("wrapTextWithPrefix() = %q, want %q", got, want)
	}
}

func TestRenderQRStopsGrowingWhenSpaceAllows(t *testing.T) {
	currentTheme = Monokai
	rehashStyles()

	small := renderQR("test-qr-payload", 20, 10)
	large := renderQR("test-qr-payload", 80, 40)

	smallFirst := strings.Split(small, "\n")[0]
	largeFirst := strings.Split(large, "\n")[0]
	if len(largeFirst) != len(smallFirst) {
		t.Fatalf("expected QR width to stop growing, small=%d large=%d", len(smallFirst), len(largeFirst))
	}
}

func TestAPICommandsSurfaceBackendErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/chats":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"bad api token"}`))
		case "/contacts":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"contacts store unavailable"}`))
		case "/messages":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"not connected"}`))
		case "/whitelist":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"db offline"}`))
		case "/media/download":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"media not found"}`))
		case "/names/set":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"phone is required"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := srv.Client()

	if msg := getChats(client, srv.URL)(); !strings.Contains(msg.(chatsMsg).err.Error(), "401 Unauthorized: bad api token") {
		t.Fatalf("unexpected chats error: %v", msg.(chatsMsg).err)
	}
	if msg := getContacts(client, srv.URL)(); !strings.Contains(msg.(contactsMsg).err.Error(), "500 Internal Server Error: contacts store unavailable") {
		t.Fatalf("unexpected contacts error: %v", msg.(contactsMsg).err)
	}
	if msg := getMsgs(client, srv.URL, "chat-1", 10)(); !strings.Contains(msg.(msgsMsg).err.Error(), "409 Conflict: not connected") {
		t.Fatalf("unexpected messages error: %v", msg.(msgsMsg).err)
	}
	if msg := getWhitelist(client, srv.URL)(); !strings.Contains(msg.(whitelistLoadMsg).err.Error(), "500 Internal Server Error: db offline") {
		t.Fatalf("unexpected whitelist error: %v", msg.(whitelistLoadMsg).err)
	}
	if msg := downloadMedia(client, srv.URL, "chat-1", "msg-1")(); !strings.Contains(msg.(mediaDownloadMsg).err.Error(), "404 Not Found: media not found") {
		t.Fatalf("unexpected media error: %v", msg.(mediaDownloadMsg).err)
	}
	if msg := setName(client, srv.URL, "", "Alex")(); !strings.Contains(msg.(whitelistSetMsg).err.Error(), "400 Bad Request: phone is required") {
		t.Fatalf("unexpected rename error: %v", msg.(whitelistSetMsg).err)
	}
}
