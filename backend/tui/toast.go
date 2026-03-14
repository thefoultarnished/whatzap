package main

import tea "github.com/charmbracelet/bubbletea"

func showToastCmd(title, body string) tea.Cmd {
	return func() tea.Msg {
		showToast(title, body)
		return nil
	}
}
