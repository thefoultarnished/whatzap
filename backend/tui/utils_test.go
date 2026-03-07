package main

import (
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
