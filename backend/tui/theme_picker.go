package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var themeList = []struct {
	name        string
	displayName string
	theme       Theme
}{
	{"linen", "Linen", Linen},
	{"tokyonight", "Tokyo Night", TokyoNight},
	{"catppuccin", "Catppuccin", Catppuccin},
	{"monokai", "Monokai", Monokai},
	{"charcoal", "Charcoal", Charcoal},
	{"aurora", "Aurora", Aurora},
	{"sakura", "Sakura", Sakura},
	{"abyssal", "Abyssal", Abyssal},
}

func (x *m) openThemePicker() {
	x.themePickerOpen = true
	x.themePickerOriginal = currentConfig.ThemeName
	// Start cursor on current theme.
	for i, t := range themeList {
		if t.name == currentConfig.ThemeName {
			x.themePickerIdx = i
			break
		}
	}
	x.leftInput = ""
	x.leftInputFocused = false
	x.mainCache.result = ""
}

func (x *m) closeThemePicker(confirm bool) {
	x.themePickerOpen = false
	if confirm {
		saveConfig()
	} else {
		// Restore original theme.
		applyThemeByName(x.themePickerOriginal)
		x.mainCache.result = ""
	}
}

func applyThemeByName(name string) {
	for _, t := range themeList {
		if t.name == name {
			currentTheme = t.theme
			currentConfig.ThemeName = name
			rehashStyles()
			return
		}
	}
}

func (x m) handleThemePicker(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyUp:
		x.themePickerIdx = (x.themePickerIdx - 1 + len(themeList)) % len(themeList)
		applyThemeByName(themeList[x.themePickerIdx].name)
		x.mainCache.result = ""
	case tea.KeyDown:
		x.themePickerIdx = (x.themePickerIdx + 1) % len(themeList)
		applyThemeByName(themeList[x.themePickerIdx].name)
		x.mainCache.result = ""
	case tea.KeyEnter:
		x.closeThemePicker(true)
	case tea.KeyEsc:
		x.closeThemePicker(false)
	}
	return x, nil
}

func (x m) renderThemePickerPane(w, h int) string {
	pickerW := min(54, max(44, w/2))

	titleStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(muted)
	activeStyle := lipgloss.NewStyle().Foreground(brand).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(text)
	divStyle := lipgloss.NewStyle().Foreground(muted)
	keyStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)

	hint := hintStyle.Render("  ") +
		keyStyle.Render("↑↓") + hintStyle.Render(" navigate  ") +
		keyStyle.Render("Enter") + hintStyle.Render(" confirm  ") +
		keyStyle.Render("Esc") + hintStyle.Render(" cancel")

	lines := []string{}
	lines = append(lines, titleStyle.Render("  Select Theme"))
	lines = append(lines, divStyle.Render("  "+strings.Repeat("─", pickerW-4)))
	for i, t := range themeList {
		if i == x.themePickerIdx {
			lines = append(lines, activeStyle.Render(fmt.Sprintf("  ▶ %s", t.displayName)))
		} else {
			lines = append(lines, inactiveStyle.Render(fmt.Sprintf("    %s", t.displayName)))
		}
	}
	lines = append(lines, divStyle.Render("  "+strings.Repeat("─", pickerW-4)))
	lines = append(lines, hint)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Width(pickerW).
		Render(strings.Join(lines, "\n"))

	return lipgloss.NewStyle().
		Width(w).
		Height(max(1, h)).
		Render(lipgloss.Place(w, max(1, h), lipgloss.Center, lipgloss.Center, box))
}
