package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	loadConfig()

	switch currentConfig.ThemeName {
	case "aurora":
		currentTheme = Aurora
	case "catppuccin":
		currentTheme = Catppuccin
	case "charcoal":
		currentTheme = Charcoal
	case "monokai":
		currentTheme = Monokai
	default:
		currentTheme = TokyoNight
	}
	rehashStyles()

	backendDir := detectDirs()
	for {
		model := m{
			baseURL:        "http://127.0.0.1:8787",
			wsURL:          "ws://127.0.0.1:8787/ws",
			backendDir:     backendDir,
			client:         &http.Client{Timeout: 12 * time.Second},
			status:         "Starting backend...",
			mode:           "nav",
			sidebarTab:     "chats",
			contacts:       map[string]contact{},
			msgs:           map[string][]wireMsg{},
			whitelist:      map[string]string{},
			names:          map[string]string{},
			sel:            0,
			scroll:         0,
			sideScroll:     0,
			active:         "",
			search:         "",
			input:          "",
			err:            "",
			topBarMsg:      "",
			topBarShown:    0,
			topBarVer:      0,
			cursorOn:       true,
			pulseOn:        false,
			flashUntil:     map[string]time.Time{},
			sidebarFocused: false,
			startedBackend: false,
			mouseEnabled:   currentConfig.MouseEnabled,
			mainCache:      &renderCache{},
		}

		opts := []tea.ProgramOption{tea.WithAltScreen()}
		if currentConfig.MouseEnabled {
			opts = append(opts, tea.WithMouseCellMotion())
		}

		p := tea.NewProgram(model, opts...)
		out, err := p.Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if fm, ok := out.(m); ok {
			fm.cleanup()
			if fm.restartRequested {
				continue
			}
		}
		break
	}
}

func detectDirs() string {
	cwd, _ := os.Getwd()
	cands := []string{cwd, filepath.Join(cwd, ".."), filepath.Join(cwd, "..", "..")}
	for _, c := range cands {
		abs, _ := filepath.Abs(c)
		if exists(filepath.Join(abs, "backend", "main.go")) {
			return filepath.Join(abs, "backend")
		}
	}
	abs, _ := filepath.Abs(cwd)
	return filepath.Join(abs, "backend")
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }
