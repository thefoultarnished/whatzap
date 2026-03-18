package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var pointerList = []struct {
	icon        string
	displayName string
}{
	{"✦", "Sparkle"},
	{"▸", "Triangle"},
	{"➤", "Arrow"},
	{"◉", "Bullseye"},
	{"●", "Circle"},
	{"◆", "Diamond"},
	{"■", "Square"},
	{"★", "Star"},
	{"►", "Play"},
	{"⬥", "Small Diamond"},
	{"⏵", "Right Triangle"},
	{"⊳", "Open Triangle"},
	{"⦿", "Target"},
	{"❯", "Chevron"},
	{"⁕", "Asterisk"},
}

// pointerRows returns the number of rows in the two-column layout.
func pointerRows() int {
	return (len(pointerList) + 1) / 2
}

// pointerColRow converts a flat index to (col, row).
func pointerColRow(idx int) (col, row int) {
	rows := pointerRows()
	if idx < rows {
		return 0, idx
	}
	return 1, idx - rows
}

// pointerIdx converts (col, row) back to a flat index.
func pointerIdx(col, row int) int {
	rows := pointerRows()
	if col == 0 {
		return row
	}
	return rows + row
}

// pointerRightColLen returns how many items are in the right column.
func pointerRightColLen() int {
	return len(pointerList) - pointerRows()
}

func (x *m) openPointerPicker() {
	x.pointerPickerOpen = true
	x.pointerPickerOriginal = receivedMsgIcon
	for i, p := range pointerList {
		if p.icon == receivedMsgIcon {
			x.pointerPickerIdx = i
			break
		}
	}
	x.leftInput = ""
	x.leftInputFocused = false
	x.mainCache.result = ""
}

func (x *m) closePointerPicker(confirm bool) {
	x.pointerPickerOpen = false
	if confirm {
		currentConfig.PointerIcon = receivedMsgIcon
		saveConfig()
	} else {
		receivedMsgIcon = x.pointerPickerOriginal
		x.mainCache.result = ""
	}
}

func (x *m) applyPointerIdx() {
	receivedMsgIcon = pointerList[x.pointerPickerIdx].icon
	x.mainCache.result = ""
}

func (x m) handlePointerPicker(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	col, row := pointerColRow(x.pointerPickerIdx)
	rows := pointerRows()
	rightLen := pointerRightColLen()

	switch k.Type {
	case tea.KeyUp:
		row--
		if row < 0 {
			if col == 0 {
				row = rows - 1
			} else {
				row = rightLen - 1
			}
		}
		x.pointerPickerIdx = pointerIdx(col, row)
		x.applyPointerIdx()
	case tea.KeyDown:
		row++
		maxRow := rows
		if col == 1 {
			maxRow = rightLen
		}
		if row >= maxRow {
			row = 0
		}
		x.pointerPickerIdx = pointerIdx(col, row)
		x.applyPointerIdx()
	case tea.KeyLeft, tea.KeyRight:
		if col == 0 {
			// Move to right column, clamp row
			if row >= rightLen {
				row = rightLen - 1
			}
			x.pointerPickerIdx = pointerIdx(1, row)
		} else {
			// Move to left column
			x.pointerPickerIdx = pointerIdx(0, row)
		}
		x.applyPointerIdx()
	case tea.KeyEnter:
		x.closePointerPicker(true)
	case tea.KeyEsc:
		x.closePointerPicker(false)
	}
	return x, nil
}

func (x m) renderPointerPickerPane(w, h int) string {
	pickerW := min(54, max(44, w/2))
	colW := (pickerW - 6) / 2 // width for each column

	titleStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(muted)
	activeStyle := lipgloss.NewStyle().Foreground(brand).Bold(true)
	inactiveStyle := lipgloss.NewStyle().Foreground(text)
	divStyle := lipgloss.NewStyle().Foreground(muted)
	keyStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)

	hint := hintStyle.Render("  ") +
		keyStyle.Render("↑↓←→") + hintStyle.Render(" navigate  ") +
		keyStyle.Render("Enter") + hintStyle.Render(" confirm  ") +
		keyStyle.Render("Esc") + hintStyle.Render(" cancel")

	rows := pointerRows()
	rightLen := pointerRightColLen()

	lines := []string{}
	lines = append(lines, titleStyle.Render("  Select Pointer Icon"))
	lines = append(lines, divStyle.Render("  "+strings.Repeat("─", pickerW-4)))

	for r := range rows {
		leftIdx := r
		var leftCell, rightCell string

		// Left column
		p := pointerList[leftIdx]
		label := fmt.Sprintf("%s  %s", p.icon, p.displayName)
		if leftIdx == x.pointerPickerIdx {
			leftCell = activeStyle.Render(fmt.Sprintf("▶ %s", label))
		} else {
			leftCell = inactiveStyle.Render(fmt.Sprintf("  %s", label))
		}
		// Pad left cell to column width
		leftCell = lipgloss.NewStyle().Width(colW).Render(leftCell)

		// Right column
		if r < rightLen {
			rightIdx := pointerIdx(1, r)
			p2 := pointerList[rightIdx]
			label2 := fmt.Sprintf("%s  %s", p2.icon, p2.displayName)
			if rightIdx == x.pointerPickerIdx {
				rightCell = activeStyle.Render(fmt.Sprintf("▶ %s", label2))
			} else {
				rightCell = inactiveStyle.Render(fmt.Sprintf("  %s", label2))
			}
		}

		lines = append(lines, fmt.Sprintf("  %s  %s", leftCell, rightCell))
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
