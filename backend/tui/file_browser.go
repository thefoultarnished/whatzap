package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fileBrowserEntry struct {
	path          string
	name          string
	isDir         bool
	isPlaceholder bool
}

func defaultFileBrowserDir() string {
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		downloads := filepath.Join(home, "Downloads")
		if info, err := os.Stat(downloads); err == nil && info.IsDir() {
			return downloads
		}
		return home
	}
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		return cwd
	}
	return "."
}

func (e fileBrowserEntry) label() string {
	switch {
	case e.isPlaceholder:
		return e.name
	case e.isDir:
		return "[" + e.name + "]"
	default:
		return e.name
	}
}

func readFileBrowserEntries(dir string) ([]fileBrowserEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	out := make([]fileBrowserEntry, 0, len(entries)+1)
	parent := filepath.Dir(dir)
	if parent != "" && parent != dir {
		out = append(out, fileBrowserEntry{
			path:  parent,
			name:  "..",
			isDir: true,
		})
	}
	for _, entry := range entries {
		out = append(out, fileBrowserEntry{
			path:  filepath.Join(dir, entry.Name()),
			name:  entry.Name(),
			isDir: entry.IsDir(),
		})
	}
	if len(out) == 0 {
		out = append(out, fileBrowserEntry{
			name:          "(empty folder)",
			isPlaceholder: true,
		})
	}
	return out, nil
}

func (x *m) loadFileBrowserDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	entries, err := readFileBrowserEntries(abs)
	if err != nil {
		return err
	}
	x.fileBrowserDir = abs
	x.fileBrowserEntries = entries
	x.fileBrowserIndex = 0
	x.fileBrowserScroll = 0
	if x.mainCache != nil {
		x.mainCache.result = ""
	}
	return nil
}

func (x *m) openFileBrowser() tea.Cmd {
	startDir := x.fileBrowserDir
	if strings.TrimSpace(startDir) == "" {
		startDir = defaultFileBrowserDir()
	}
	if err := x.loadFileBrowserDir(startDir); err != nil {
		return x.setTopBar(fmt.Sprintf("File browser: %v", err))
	}
	x.fileBrowserOpen = true
	return nil
}

func (x *m) closeFileBrowser() {
	x.fileBrowserOpen = false
	if x.mainCache != nil {
		x.mainCache.result = ""
	}
}

func (x *m) ensureFileBrowserVisible(viewRows int) {
	if len(x.fileBrowserEntries) == 0 {
		x.fileBrowserIndex = 0
		x.fileBrowserScroll = 0
		return
	}
	if x.fileBrowserIndex < 0 {
		x.fileBrowserIndex = 0
	}
	if x.fileBrowserIndex >= len(x.fileBrowserEntries) {
		x.fileBrowserIndex = len(x.fileBrowserEntries) - 1
	}
	if viewRows <= 0 {
		return
	}
	maxStart := max(0, len(x.fileBrowserEntries)-viewRows)
	if x.fileBrowserScroll > maxStart {
		x.fileBrowserScroll = maxStart
	}
	if x.fileBrowserScroll < 0 {
		x.fileBrowserScroll = 0
	}
	if x.fileBrowserIndex < x.fileBrowserScroll {
		x.fileBrowserScroll = x.fileBrowserIndex
	}
	if x.fileBrowserIndex >= x.fileBrowserScroll+viewRows {
		x.fileBrowserScroll = x.fileBrowserIndex - viewRows + 1
	}
}

func (x m) fileBrowserVisibleRows(h int) int {
	return max(1, h-6)
}

func (x m) selectedFileBrowserEntry() (fileBrowserEntry, bool) {
	if len(x.fileBrowserEntries) == 0 || x.fileBrowserIndex < 0 || x.fileBrowserIndex >= len(x.fileBrowserEntries) {
		return fileBrowserEntry{}, false
	}
	return x.fileBrowserEntries[x.fileBrowserIndex], true
}

func quoteSendPath(path string) string {
	return strings.ReplaceAll(path, `"`, `\"`)
}

func (x m) renderFileBrowser(w, h int) string {
	titleStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(muted)
	dirStyle := lipgloss.NewStyle().Foreground(text)
	activeStyle := lipgloss.NewStyle().Foreground(buttonInk).Background(accent).Bold(true)
	dirEntryStyle := lipgloss.NewStyle().Foreground(brand).Bold(true)
	fileEntryStyle := lipgloss.NewStyle().Foreground(text)

	lines := []string{
		titleStyle.Render(" File Browser"),
		hintStyle.Render(" " + truncateDisplayWidth(x.fileBrowserDir, max(10, w-2))),
		hintStyle.Render(strings.Repeat("─", max(8, w-2))),
	}

	rows := x.fileBrowserVisibleRows(h)
	xCopy := x
	xCopy.ensureFileBrowserVisible(rows)
	start := xCopy.fileBrowserScroll
	end := min(len(xCopy.fileBrowserEntries), start+rows)
	for i := start; i < end; i++ {
		entry := xCopy.fileBrowserEntries[i]
		label := truncateDisplayWidth(entry.label(), max(4, w-6))
		if i == xCopy.fileBrowserIndex {
			lines = append(lines, activeStyle.Width(max(4, w-2)).Render("▶ "+label))
			continue
		}
		style := fileEntryStyle
		if entry.isDir && !entry.isPlaceholder {
			style = dirEntryStyle
		}
		lines = append(lines, style.Render("  "+label))
	}
	for len(lines) < max(1, h-2) {
		lines = append(lines, "")
	}

	status := fmt.Sprintf(" %d/%d", min(len(x.fileBrowserEntries), x.fileBrowserIndex+1), len(x.fileBrowserEntries))
	lines = append(lines, hintStyle.Render(strings.Repeat("─", max(8, w-2))))
	lines = append(lines, dirStyle.Render(" Enter open/select  Backspace up  Esc close  ↑↓ scroll"+status))

	return lipgloss.NewStyle().
		Width(w).
		Height(max(1, h)).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}
