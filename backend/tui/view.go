package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const successLoadingScreenName = "Success Loading Screen"

func (x m) View() string {
	if x.w == 0 || x.h == 0 {
		return "loading..."
	}
	frameW := max(1, x.w-2)
	// Success Loading Screen: shown after login until status becomes "ready".
	if x.status != "ready" {
		outerW := frameW
		outerH := max(3, x.h-2)
		innerW := max(1, outerW-2)
		innerH := max(1, outerH-2)
		statusBody := x.status
		title := logoStyle.Render("WhatZap")
		subtitle := mutedStyle.Render("Private WhatsApp in your terminal")
		hint := mutedStyle.Render("Keep this window open")
		statusMsgTemplate := func(body, progress string) string {
			return title + "\n" + subtitle + "\n\n" + body + "\n" + progress + "\n\n" + hint
		}
		if x.status == "qr" {
			if x.qrRaw != "" {
				hint = accentStyle.Copy().Bold(false).Render("WhatsApp > Linked Devices > Link a Device")
				waiting := logoStyle.Render(spinnerFrames[x.spinnerFrame] + " waiting for scan")
				qrMaxW := min(max(12, innerW-6), 56)
				qrMaxH := min(max(8, innerH-6), 28)
				qrBody := renderQR(x.qrRaw, qrMaxW, qrMaxH)
				if qrBody == "" {
					qrBody = x.qrRaw
				}
				body := lipgloss.JoinVertical(
					lipgloss.Center,
					waiting,
					"",
					lipgloss.PlaceHorizontal(innerW, lipgloss.Center, qrBody),
					"",
					hint,
				)
				content := lipgloss.Place(innerW, innerH, lipgloss.Center, lipgloss.Center, body)
				return lipgloss.NewStyle().
					Width(outerW).
					Height(outerH).
					Border(lipgloss.RoundedBorder()).
					BorderForeground(brand).
					Render(content)
			} else {
				statusBody = "Generating QR..."
				hint = mutedStyle.Render("Preparing login QR")
			}
		} else if strings.HasPrefix(strings.ToLower(x.status), "logged out") {
			statusBody = accentStyle.Copy().Bold(false).Render(x.status)
			hint = mutedStyle.Render("Restart and scan a QR code to sign in again")
		} else {
			statusBody = accentStyle.Copy().Bold(false).Render(spinnerFrames[x.spinnerFrame] + " Connecting your session...")
			progress := mutedStyle.Render("Syncing chats, contacts, and recent messages")
			pulse := x.loadingPulse()
			body := statusMsgTemplate(statusBody, progress+"\n"+pulse)
			content := lipgloss.Place(innerW, innerH, lipgloss.Center, lipgloss.Center, body)
			return lipgloss.NewStyle().
				Width(outerW).
				Height(outerH).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(brand).
				Render(content)
		}
		msg := statusMsgTemplate(statusBody, "")
		content := lipgloss.Place(innerW, innerH, lipgloss.Center, lipgloss.Center, msg)
		return lipgloss.NewStyle().
			Width(outerW).
			Height(outerH).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(brand).
			Render(content)
	}
	outerW := frameW
	outerH := x.h - 2
	contentW := outerW
	leftW := min(28, max(24, contentW/3))
	rightW := contentW - leftW
	head := x.renderHeaderContainer(contentW, leftW)
	replyBarH := 0
	if x.replyTo != nil {
		replyBarH = 1
	}
	side := x.renderSide(leftW, outerH-4-replyBarH)

	hasFlash := false
	now := time.Now()
	for _, until := range x.flashUntil {
		if now.Before(until) {
			hasFlash = true
			break
		}
	}
	lastMsgID := ""
	if msgs := x.msgs[x.active]; len(msgs) > 0 {
		lastMsgID = msgs[len(msgs)-1].Key.ID
	}
	replyToID := ""
	if x.replyTo != nil {
		replyToID = x.replyTo.Key.ID
	}
	cacheKey := mainCacheKey{
		active:       x.active,
		themeName:    currentConfig.ThemeName,
		msgCount:     len(x.msgs[x.active]),
		lastMsgID:    lastMsgID,
		scroll:       x.scroll,
		w:            x.w,
		h:            x.h,
		atInput:      "",
		replyToID:    replyToID,
		selectedMsg:  x.selectedMsgID,
		contactCount: len(x.contacts) + len(x.names),
		identityVer:  x.identityVersion,
		spinnerFrame: x.spinnerFrame,
	}
	var main string
	if x.emojiPickerOpen {
		main = x.renderEmojiPickerPane(rightW, outerH-4-replyBarH)
	} else if !hasFlash && x.mainCache.result != "" && x.mainCache.key == cacheKey {
		main = x.mainCache.result
	} else {
		main = x.renderMain(rightW, outerH-4-replyBarH)
		if !hasFlash {
			x.mainCache.key = cacheKey
			x.mainCache.result = main
		}
	}

	center := lipgloss.JoinHorizontal(lipgloss.Top, side, main)
	typedInput := x.input + x.inputBuf
	splitInputBar := lipgloss.JoinHorizontal(lipgloss.Bottom, x.renderCommandBox(leftW), x.renderChatInput(rightW, typedInput))

	bodyParts := []string{center}
	if replyBar := x.renderReplyBar(contentW, rightW); replyBar != "" {
		bodyParts = append(bodyParts, replyBar)
	}
	bodyParts = append(bodyParts, splitInputBar)
	body := lipgloss.JoinVertical(lipgloss.Left, bodyParts...)
	inner := lipgloss.JoinVertical(lipgloss.Left, head, body)
	frame := lipgloss.NewStyle().
		Width(outerW).
		Height(outerH).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(muted).
		Render(inner)
	return frame
}

func (x m) loadingPulse() string {
	steps := []string{
		"●○○",
		"○●○",
		"○○●",
	}
	idx := x.spinnerFrame % len(steps)
	return accentStyle.Copy().Bold(false).Render(steps[idx])
}

func (x m) renderHeaderContainer(contentW, leftW int) string {
	totalUnread := 0
	for _, c := range x.chats {
		totalUnread += c.UnreadCount
	}

	logo := " " + logoStyle.Render("WhatZap")
	statusPart := ""
	if totalUnread > 0 {
		statusPart = amberStyle.Render(strconv.Itoa(totalUnread)+" unread") + " "
	} else {
		wifi := lipgloss.NewStyle().Foreground(brand).Render("ᯤ")
		connText := lipgloss.NewStyle().Foreground(brand).Render(" connected")
		if x.demoMode {
			connText = lipgloss.NewStyle().Foreground(brand).Render(" demo")
		}
		statusPart = " " + wifi + connText + " "
	}

	statusW := lipgloss.Width(statusPart)
	padW := leftW - statusW
	if padW < 0 {
		padW = 0
	}
	logoBlock := lipgloss.NewStyle().Width(padW).Render(logo)
	leftContent := logoBlock + statusPart
	leftStr := leftContent + lipgloss.NewStyle().Foreground(muted).Render("|")

	themeMood := lipgloss.NewStyle().
		Foreground(badgeInk).
		Background(anomalyTag).
		Bold(true).
		Render(" ◉ MOOD ")
	themeName := lipgloss.NewStyle().
		Foreground(badgeInk).
		Background(accent).
		Bold(true).
		Render(" " + strings.ToUpper(currentConfig.ThemeName) + " ")
	rightStr := themeMood + themeName + " "
	rightVW := lipgloss.Width(rightStr)
	centerW := max(0, contentW-(leftW+1)-rightVW)

	centerContent := " "
	if x.topBarMsg != "" && x.topBarShown > 0 {
		centerContent = " " + purpleStyle.Render(graphemeSliceN(x.topBarMsg, x.topBarShown))
	} else if x.active != "" {
		displayName := x.nameFor(x.active)
		nameLimit := 22
		if centerW >= 24 {
			avatar := renderHeaderAvatar(displayName, x.active)
			nameLimit = 18
			centerContent = " " + avatar + " " + accentStyle.Render(truncate(displayName, nameLimit))
		} else {
			centerContent = " " + accentStyle.Render(truncate(displayName, nameLimit))
		}
		if time.Now().Before(x.msgActivityUntil) && x.msgActivityType == "sent" {
			centerContent += accentStyle.Copy().Bold(false).Render("  " + spinnerFrames[x.spinnerFrame] + " message sent")
		}
	}

	centerStr := lipgloss.NewStyle().Width(centerW).Render(centerContent)
	headLine := leftStr + centerStr + rightStr
	return lipgloss.NewStyle().
		Width(contentW).
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(muted).
		Render(headLine)
}

func renderHeaderAvatar(name, id string) string {
	initials := headerAvatarInitials(name, id)
	return lipgloss.NewStyle().
		Foreground(buttonInk).
		Background(brand).
		Bold(true).
		Render(" " + initials + " ")
}

func headerAvatarInitials(name, id string) string {
	parts := strings.Fields(strings.TrimSpace(name))
	initials := make([]rune, 0, 2)
	for _, part := range parts {
		for _, r := range part {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				initials = append(initials, unicode.ToUpper(r))
				break
			}
		}
		if len(initials) == 2 {
			break
		}
	}
	if len(initials) == 0 {
		n := num(id)
		rs := []rune(n)
		if len(rs) >= 2 {
			initials = append(initials, rs[len(rs)-2], rs[len(rs)-1])
		} else if len(rs) == 1 {
			initials = append(initials, rs[0])
		}
	}
	if len(initials) == 1 {
		initials = append(initials, ' ')
	}
	if len(initials) == 0 {
		return "??"
	}
	return string(initials[:2])
}

func (x m) renderCommandBox(leftW int) string {
	cmdBadge := cmdBadgeStyle.Render(" CMD ")
	cmdContent := cmdBadge + lipgloss.NewStyle().Foreground(text).Render(x.leftInput)
	if x.leftInputFocused {
		trimmedLeft := strings.TrimSpace(x.leftInput)
		ghost := ""
		if x.leftInput != "" {
			if best := commandBestMatch(x.leftInput); best != "" && strings.HasPrefix(best, strings.ToLower(trimmedLeft)) {
				ghost = best[len(trimmedLeft):]
			}
		}
		if ghost != "" {
			firstGhost, restGhost := graphemeSplitFirst(ghost)
			if x.cursorOn {
				cmdContent += cursorStyle.Render(firstGhost)
			} else {
				cmdContent += ghostStyle.Render(firstGhost)
			}
			cmdContent += ghostStyle.Render(restGhost)
		} else if x.cursorOn {
			cmdContent += lipgloss.NewStyle().Foreground(cursorColor).Render("|")
		} else {
			cmdContent += " "
		}
	}
	if !x.leftInputFocused && x.leftInput == "" {
		cmdContent = cmdBadge + ghostStyle.Render(" Ctrl+K for commands")
	}
	leftBorderColor := muted
	if x.leftInputFocused {
		leftBorderColor = accent
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, true, false, false).
		BorderForeground(leftBorderColor).
		Width(leftW).
		Foreground(text).
		Render(cmdContent)
}

func (x m) renderChatInput(rightW int, typedInput string) string {
	trimmedInput := strings.TrimSpace(typedInput)
	isCmd := strings.HasPrefix(trimmedInput, "/")
	rightFocused := x.mode == "chat" && !x.sidebarFocused && !x.leftInputFocused
	inputLocked := x.chatInputLocked()
	inputGhost := ""
	if rightFocused && !inputLocked && !x.inputAllSelected {
		inputGhost = chatInputGhost(typedInput)
	}

	showSend := hasVisibleText(typedInput) && !isCmd && rightFocused && !inputLocked
	sendBadge := ""
	sendBadgeW := 0
	if showSend {
		sendBadge = lipgloss.NewStyle().Foreground(buttonInk).Background(brand).Bold(true).Render(" SEND ")
		sendBadgeW = lipgloss.Width(sendBadge)
	}

	textAreaW := max(1, rightW-1-sendBadgeW)
	var inputDisplay string
	if inputLocked {
		inputDisplay = lipgloss.NewStyle().Foreground(muted).Render(" blacklisted | Ctrl+K then /whitelist")
	} else if x.inputAllSelected {
		inputDisplay = lipgloss.NewStyle().Foreground(text).Render(" ") +
			lipgloss.NewStyle().Foreground(buttonInk).Background(accent).Render(typedInput)
	} else {
		inputDisplay = lipgloss.NewStyle().Foreground(text).Render(" " + typedInput)
		if rightFocused && inputGhost != "" {
			firstGhost, restGhost := graphemeSplitFirst(inputGhost)
			if x.cursorOn {
				inputDisplay += cursorStyle.Render(firstGhost)
			} else {
				inputDisplay += ghostStyle.Render(firstGhost)
			}
			inputDisplay += ghostStyle.Render(restGhost)
		} else if rightFocused && x.cursorOn {
			inputDisplay += inputCursorStyle.Render(inputCursorGlyph)
		} else if rightFocused {
			inputDisplay += " "
		}
	}

	if isCmd {
		inputDisplay += lipgloss.NewStyle().Foreground(accent).Bold(true).Render("  CMD")
	}
	if x.replyPickMode {
		inputDisplay = lipgloss.NewStyle().Foreground(accent).Bold(true).Render(" R") +
			lipgloss.NewStyle().Foreground(text).Render("/") +
			lipgloss.NewStyle().Foreground(accent).Bold(true).Render("Enter") +
			lipgloss.NewStyle().Foreground(text).Render(" quote reply  ") +
			lipgloss.NewStyle().Foreground(accent).Bold(true).Render("Esc") +
			lipgloss.NewStyle().Foreground(text).Render("/") +
			lipgloss.NewStyle().Foreground(accent).Bold(true).Render("Alt") +
			lipgloss.NewStyle().Foreground(text).Render(" cancel")
	} else if x.mode == "nav" && x.active == "" {
		inputDisplay = lipgloss.NewStyle().Foreground(muted).Render(" Press Enter/Tab to start chatting")
	} else if x.mode == "nav" || (x.mode == "chat" && x.sidebarFocused) {
		inputDisplay = lipgloss.NewStyle().Foreground(muted).Render(" Press Enter/Tab to start chatting")
	} else if inputLocked {
		inputDisplay = lipgloss.NewStyle().Foreground(muted).Render(" blacklisted | Ctrl+K then /whitelist")
	} else if rightFocused && !x.emojiPickerOpen && typedInput == "" {
		if x.cursorOn {
			inputDisplay = lipgloss.NewStyle().Foreground(muted).Render(" ") +
				inputCursorStyle.Render(inputCursorGlyph) +
				lipgloss.NewStyle().Foreground(muted).Render("Type a message | Alt+E for emoji")
		} else {
			inputDisplay = lipgloss.NewStyle().Foreground(muted).Render("  Type a message | Alt+E for emoji")
		}
	}

	rightContent := lipgloss.NewStyle().Width(textAreaW).Render(inputDisplay) + sendBadge
	rightBorderColor := muted
	if rightFocused && x.active != "" {
		if _, ok := x.whitelist[num(x.active)]; ok {
			rightBorderColor = brand
		} else {
			rightBorderColor = red
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(rightBorderColor).
		Foreground(text).
		Render(rightContent)
}

func (x m) renderReplyBar(contentW, rightW int) string {
	displayReplyTo := x.replyTo
	if displayReplyTo == nil {
		return ""
	}
	leftW := contentW - rightW

	rSender := x.senderNameForMsg(*displayReplyTo)
	rText := renderMessageBody(displayReplyTo.Message)
	maxText := rightW - 20
	if maxText < 20 {
		maxText = 20
	}
	if len([]rune(rText)) > maxText {
		rText = string([]rune(rText)[:maxText]) + "..."
	}
	quoteTextColor := quotedReceivedText
	nameColor := receivedName
	if displayReplyTo.Key.FromMe {
		quoteTextColor = quotedSentText
		nameColor = sentName
	}
	bar := lipgloss.NewStyle().Foreground(accent).Render(" ╭─ ") +
		lipgloss.NewStyle().Foreground(nameColor).Bold(true).Render(rSender+": ") +
		lipgloss.NewStyle().Foreground(quoteTextColor).Render(rText) +
		lipgloss.NewStyle().Foreground(muted).Render("  Esc cancel")
	return strings.Repeat(" ", leftW) + lipgloss.NewStyle().Width(rightW).Render(bar)
}

func (x m) renderSide(w, h int) string {
	f := x.filtered()
	tabW := max(8, (w-3)/2)
	inactiveTabStyle := lipgloss.NewStyle().
		Width(tabW).
		Align(lipgloss.Center).
		Foreground(muted)
	activeTabStyle := lipgloss.NewStyle().
		Width(tabW).
		Align(lipgloss.Center).
		Foreground(buttonInk).
		Background(accent).
		Bold(true)
	labelStyle := lipgloss.NewStyle().Bold(true)
	shortcutStyle := lipgloss.NewStyle().Foreground(muted)
	activeShortcutStyle := lipgloss.NewStyle().Foreground(shortcutActive)
	chatsTab := inactiveTabStyle.Render(labelStyle.Render("Chats") + " " + shortcutStyle.Render("alt+c"))
	contactsTab := inactiveTabStyle.Render(labelStyle.Render("People") + " " + shortcutStyle.Render("alt+p"))
	if x.sidebarTab == "chats" {
		chatsTab = activeTabStyle.Render("Chats " + activeShortcutStyle.Render("alt+c"))
	} else {
		contactsTab = activeTabStyle.Render("People " + activeShortcutStyle.Render("alt+p"))
	}
	tabsLine := chatsTab + " " + contactsTab
	underlineColor := muted
	if x.mode == "search" {
		underlineColor = accent
	}
	tabsUnderline := lipgloss.NewStyle().Foreground(muted).Render(strings.Repeat("─", max(8, w-2)))
	underline := lipgloss.NewStyle().Foreground(underlineColor).Render(strings.Repeat("─", max(8, w-2)))
	lines := []string{tabsLine, tabsUnderline, x.renderSearchBox(), underline}
	viewRows := max(1, h-4)
	maxStart := max(0, len(f)-viewRows)
	start := x.sideScroll
	if start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	end := min(len(f), start+viewRows)
	lines = append(lines, x.renderUserList(f, start, end, w)...)
	if len(f) == 0 && strings.TrimSpace(x.search) != "" {
		lines = append(lines, mutedStyle.Render("  no results"))
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().
		Width(w).
		Height(h).
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(muted).
		Render(strings.Join(lines, "\n"))
}

func (x m) renderSearchBox() string {
	searchFocused := x.mode == "search"
	searchValue := x.search
	if searchFocused {
		searchValue = x.searchInput
	}
	searchIconText := "⌕ "
	searchIcon := mutedStyle.Render(searchIconText)
	searchLine := searchIcon
	placeholder := "search [Alt+S]"
	if x.sidebarTab == "contacts" {
		placeholder = "type to search users"
	}
	if searchFocused && x.sidebarTab == "contacts" && searchValue == "" {
		if x.cursorOn {
			searchLine += inputCursorStyle.Render(inputCursorGlyph)
		} else {
			searchLine += " "
		}
		searchLine += mutedStyle.Render(placeholder)
		return searchLine
	}
	if searchValue == "" && !searchFocused {
		searchLine += mutedStyle.Render(placeholder)
	} else if searchValue != "" {
		searchLine += lipgloss.NewStyle().Foreground(text).Render(searchValue)
	}
	if searchFocused {
		if x.cursorOn {
			searchLine += inputCursorStyle.Render(inputCursorGlyph)
		} else {
			searchLine += " "
		}
	}
	return searchLine
}

func (x m) renderUserList(f []chat, start, end, w int) []string {
	lines := []string{}
	for i := start; i < end; i++ {
		c := f[i]
		hasUnread := c.UnreadCount > 0
		isSel := i == x.sel
		navActive := x.mode != "chat" || x.sidebarFocused
		isActive := x.active == c.ID
		highlighted := isSel && navActive
		_, whitelisted := x.whitelist[num(c.ID)]

		rowBase := lipgloss.NewStyle()
		if highlighted {
			bg := brand
			if !whitelisted {
				bg = amber
			}
			rowBase = rowBase.Background(bg).Foreground(buttonInk).Bold(isSel && navActive)
		} else if isActive {
			activeBg := sidebarActiveBg
			if !whitelisted {
				activeBg = sidebarActiveUnreadBg
			}
			rowBase = rowBase.Background(activeBg).Foreground(accent)
		}

		rowWidth := max(1, w-2)
		nameWidth := max(1, rowWidth-1)

		nameText := fmt.Sprintf("%d. %s", i+1, x.name(c))
		if highlighted && graphemeCount(nameText) > nameWidth {
			offset := x.sidebarMarqueeOffset
			maxOffset := graphemeCount(nameText) - nameWidth
			if offset < 0 {
				offset = 0
			}
			if offset > maxOffset {
				offset = maxOffset
			}
			nameText = graphemeWindow(nameText, offset, nameWidth)
		} else {
			nameText = truncate(nameText, nameWidth)
		}
		if highlighted {
			nameText = strings.Repeat(" ", x.sidebarHighlightInset) + nameText
		}
		line := rowBase.Render(padRight(nameText, nameWidth))
		if hasUnread {
			line += rowBase.Copy().Foreground(amber).Render("●")
		} else {
			line += rowBase.Render(" ")
		}
		lines = append(lines, line)
	}
	return lines
}

func chatMessageWrapWidth(w int, msg string) int {
	// Keep message content comfortably inside the pane so inline timestamps and long links don't spill.
	contentW := max(8, w-2)
	seventyPct := (contentW * 7) / 10
	base := max(8, min(seventyPct, contentW-18))
	return max(8, base-countEmojiHeuristic(msg))
}

func wrapMessageLines(msgBody string, availableW int, fromMe bool, senderName string) []string {
	if fromMe {
		return strings.Split(wrapText(msgBody, availableW), "\n")
	}
	prefixWidth := runeDisplayWidth(senderName + ": ")
	return strings.Split(wrapTextWithPrefix(msgBody, availableW, prefixWidth), "\n")
}

func renderStyledMessageText(
	ln string,
	bodyStyle lipgloss.Style,
	tokenStyle lipgloss.Style,
	isMediaMsg bool,
) string {
	if isMediaMsg && strings.HasPrefix(ln, "[") {
		if end := strings.Index(ln, "]"); end > 0 {
			token := ln[:end+1]
			rest := ln[end+1:]
			return tokenStyle.Render(token) + bodyStyle.Render(rest)
		}
	}
	return bodyStyle.Render(ln)
}

func outgoingMessageIndent(paneW int, blockW int, fromMe bool) string {
	if !fromMe {
		return ""
	}
	paneW = max(1, paneW)
	blockW = max(1, min(blockW, paneW))
	return strings.Repeat(" ", max(0, paneW-blockW))
}

func (x m) renderWelcomePane(w, h int) string {
	keyStyle := lipgloss.NewStyle().Foreground(accent).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(text)
	secStyle := lipgloss.NewStyle().Foreground(purple).Bold(true)
	divColor := lipgloss.NewStyle().Foreground(muted)

	// content column width
	col := 38
	if col > w-4 {
		col = w - 4
	}
	if col < 20 {
		col = 20
	}

	// header centered
	star := lipgloss.NewStyle().Foreground(brand).Bold(true).Render(spinnerFrames[x.spinnerFrame])
	title := lipgloss.NewStyle().Foreground(text).Bold(true).Render("WhatZap")
	tagline := mutedStyle.Render("Terminal WhatsApp client")
	header := lipgloss.JoinVertical(lipgloss.Center, star, title, tagline)
	header = lipgloss.PlaceHorizontal(col, lipgloss.Center, header)

	div := divColor.Render(strings.Repeat("─", col))

	kw := 15
	shortcut := func(key, desc string) string {
		rk := keyStyle.Render(key)
		pad := kw - len([]rune(key))
		if pad < 1 {
			pad = 1
		}
		return rk + strings.Repeat(" ", pad) + descStyle.Render(desc)
	}

	section := func(title string, items []string) string {
		h := secStyle.Render("  " + title)
		return h + "\n" + strings.Join(items, "\n")
	}

	nav := section("Navigation", []string{
		shortcut("↑ / ↓", "Browse chats"),
		shortcut("Enter", "Open chat"),
		shortcut("Esc", "Close / go back"),
		shortcut("Tab", "Toggle sidebar"),
	})

	chat := section("In Chat", []string{
		shortcut("Alt+↑ / ↓", "Switch chats"),
		shortcut("↑ / ↓", "Scroll messages"),
		shortcut("Alt+R", "Pick msg to reply"),
		shortcut("Alt+E", "Emoji picker"),
		shortcut("Tab / Esc", "Exit chat"),
	})

	quick := section("Quick Actions", []string{
		shortcut("Alt+S", "Search"),
		shortcut("Alt+C", "Chats tab"),
		shortcut("Alt+P", "Contacts tab"),
		shortcut("Ctrl+K", "Command palette"),
		shortcut("Alt+M", "Toggle mouse"),
	})

	hint := lipgloss.PlaceHorizontal(col, lipgloss.Center,
		mutedStyle.Render("Select a chat to start messaging"))

	body := lipgloss.JoinVertical(lipgloss.Left,
		"", header, "", div, "",
		nav, "", chat, "", quick,
		"", div, hint,
	)

	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, body)
}

func (x m) renderMain(w, h int) string {
	if x.active == "" {
		return x.renderWelcomePane(w, h)
	}
	items := x.msgs[x.active]
	type reactionRender struct {
		emoji string
		name  string
		isMe  bool
	}
	reactionsFor := map[string][]reactionRender{}
	for _, msg := range items {
		if rxn, ok := msg.Message["reactionMessage"].(map[string]any); ok {
			targetID, _ := rxn["targetMsgID"].(string)
			emoji, _ := rxn["emoji"].(string)
			if targetID != "" && emoji != "" {
				rSender := "Me"
				rIsMe := msg.Key.FromMe
				if !msg.Key.FromMe {
					rSender = truncate(x.senderNameForMsg(msg), 10)
				}
				reactionsFor[targetID] = append(reactionsFor[targetID], reactionRender{
					emoji: emoji,
					name:  rSender,
					isMe:  rIsMe,
				})
			}
		}
	}
	needed := h + x.scroll
	msgBlocks := [][]string{}
	msgTimestamps := []int64{}
	msgBlockMsgs := []wireMsg{}
	msgBlockTimeLine := []int{}
	for i := len(items) - 1; i >= 0; i-- {
		msg := items[i]
		msgBody := renderMessageBody(msg.Message)
		if msgBody == "" {
			msgBody = "[media]"
		}

		timeStr := formatExactTime(msg.MessageTimestamp)
		receiptText := ""
		if msg.Key.FromMe {
			switch msg.ReceiptStatus {
			case "delivered":
				receiptText = "  ✓✓ "
			case "read", "played":
				receiptText = "  ✓✓ "
			default:
				receiptText = "  ✓ "
			}
		}
		senderName := "Me"
		if !msg.Key.FromMe {
			senderName = truncate(x.senderNameForMsg(msg), 12)
		}

		isFlashing := !msg.Key.FromMe && msg.Key.ID != "" && x.flashUntil[msg.Key.ID].After(time.Now())

		timeColor := muted
		if isFlashing {
			timeColor = accent
		}

		if _, ok := msg.Message["reactionMessage"]; ok {
			continue
		}

		availableW := chatMessageWrapWidth(w, msgBody)

		wrapped := wrapMessageLines(msgBody, availableW, msg.Key.FromMe, senderName)

		bodyColor := sentText
		if !msg.Key.FromMe {
			bodyColor = receivedText
		}
		isMediaMsg := isMediaWire(msg)
		mediaTokenBG := mediaTokenBg
		if x.pulseOn {
			mediaTokenBG = mediaTokenPulseBg
		}
		if isFlashing {
			bodyColor = accent
		}

		isClickedSelected := x.selectedMsgID != "" && msg.Key.ID == x.selectedMsgID
		selectedBG := lipgloss.Color("")
		if isClickedSelected && !x.replyPickMode {
			selectedBG = messageSelectedBg
		}
		bodyBG := lipgloss.Color("")
		if isClickedSelected && x.replyPickMode {
			bodyBG = accent
		} else if isClickedSelected {
			bodyBG = messageSelectedBg
		}
		applySelectedBG := func(st lipgloss.Style) lipgloss.Style {
			if selectedBG != "" {
				return st.Background(selectedBG)
			}
			return st
		}
		applyBodyBG := func(st lipgloss.Style) lipgloss.Style {
			if bodyBG != "" {
				return st.Background(bodyBG)
			}
			return st
		}

		timeStyled := applyBodyBG(mutedStyle.Copy().Foreground(timeColor)).Render("  " + timeStr)
		if receiptText != "" {
			receiptColor := muted
			if msg.ReceiptStatus == "read" || msg.ReceiptStatus == "played" {
				receiptColor = accent
			}
			timeStyled += applyBodyBG(mutedStyle.Copy().Foreground(receiptColor).Bold(true)).Render(receiptText)
		}
		block := []string{}
		hasQuoteLine := false

		reactionLinePlain := ""
		reactionLine := ""
		if rxns, ok := reactionsFor[msg.Key.ID]; ok && len(rxns) > 0 {
			reactionPlainParts := []string{}
			reactionStyledParts := []string{}
			firstReactionColor := receivedName
			for i, rxn := range rxns {
				reactionColor := receivedName
				if rxn.isMe {
					reactionColor = sentName
				}
				if i == 0 {
					firstReactionColor = reactionColor
				}
				reactionPlainParts = append(reactionPlainParts, rxn.emoji+" "+rxn.name)
				reactionStyledParts = append(reactionStyledParts,
					applySelectedBG(lipgloss.NewStyle().Foreground(reactionColor).Bold(true)).Render(rxn.emoji+" "+rxn.name))
			}
			reactionLinePlain = "  ╰─ " + strings.Join(reactionPlainParts, ", ")
			reactionPrefix := applySelectedBG(lipgloss.NewStyle().Foreground(firstReactionColor)).Render("  ╰─ ")
			reactionLine = reactionPrefix + strings.Join(reactionStyledParts, applySelectedBG(mutedStyle).Render(", "))
		}

		quoteStyled := ""
		quoteStyledRight := ""
		quotePlainRight := ""
		if ext, ok := msg.Message["extendedTextMessage"].(map[string]any); ok {
			if qText, _ := ext["quotedText"].(string); qText != "" {
				hasQuoteLine = true
				qParticipant, _ := ext["quotedParticipant"].(string)
				qFromMe, _ := ext["quotedFromMe"].(bool)
				qSender := "Me"
				if !qFromMe && strings.TrimSpace(qParticipant) != "" {
					qSender = x.nameFor(qParticipant)
				}
				if !qFromMe && strings.TrimSpace(qSender) == "" {
					qSender = num(qParticipant)
				}
				qText = strings.ReplaceAll(qText, "\n", " ")
				if len([]rune(qText)) > 50 {
					qText = string([]rune(qText)[:50]) + "..."
				}
				qSenderColor := receivedName
				qTextColor := receivedText
				if qFromMe {
					qSenderColor = sentName
					qTextColor = sentText
				}
				quotePrefix := "  ╭─ "
				quoteSuffix := " ─╮ "
				quoteStyled = applySelectedBG(lipgloss.NewStyle().Foreground(qSenderColor)).Render(quotePrefix) +
					applySelectedBG(lipgloss.NewStyle().Foreground(qSenderColor).Bold(true)).Render(qSender+": ") +
					applySelectedBG(lipgloss.NewStyle().Foreground(qTextColor)).Render(qText)
				quoteStyledRight = applySelectedBG(lipgloss.NewStyle().Foreground(qSenderColor).Bold(true)).Render(qSender+": ") +
					applySelectedBG(lipgloss.NewStyle().Foreground(qTextColor)).Render(qText) +
					applySelectedBG(lipgloss.NewStyle().Foreground(qSenderColor)).Render(quoteSuffix)
				quotePlainRight = qSender + ": " + qText + quoteSuffix
			}
		}
		outgoingBlockW := 0
		outgoingBodyW := 0
		if msg.Key.FromMe {
			for i, ln := range wrapped {
				outgoingBodyW = max(outgoingBodyW, runeDisplayWidth(ln+" "))
				lineMeasure := ln
				if i == len(wrapped)-1 {
					lineMeasure += "  " + timeStr + receiptText + " "
				} else {
					lineMeasure += " "
				}
				outgoingBlockW = max(outgoingBlockW, runeDisplayWidth(lineMeasure))
			}
			if reactionLinePlain != "" {
				outgoingBlockW = max(outgoingBlockW, runeDisplayWidth(reactionLinePlain))
			}
			outgoingBlockW = min(outgoingBlockW, max(1, w-2))
		}
		indent := outgoingMessageIndent(max(1, w-2), outgoingBlockW, msg.Key.FromMe)
		if quoteStyled != "" {
			if msg.Key.FromMe {
				// Anchor the quote connector to message text, not the trailing receipt/time.
				qPlainW := runeDisplayWidth(quotePlainRight)
				qIndent := max(0, len(indent)+outgoingBodyW-qPlainW)
				block = append(block, strings.Repeat(" ", qIndent)+quoteStyledRight)
			} else {
				block = append(block, quoteStyled)
			}
		}
		lastBodyPlainW := 0
		for i, ln := range wrapped {
			bodyStyle := applyBodyBG(lipgloss.NewStyle().Foreground(bodyColor).Bold(isFlashing))
			tokenStyle := applyBodyBG(lipgloss.NewStyle().
				Foreground(tagInk).
				Background(mediaTokenBG).
				Bold(true))
			lineParts := []string{}
			if !msg.Key.FromMe && i == 0 {
				lineParts = append(lineParts, applyBodyBG(lipgloss.NewStyle().
					Foreground(receivedName).
					Bold(true)).
					Render(senderName+": "))
			}
			lineParts = append(lineParts, renderStyledMessageText(ln, bodyStyle, tokenStyle, isMediaMsg))
			if i == len(wrapped)-1 {
				lineParts = append(lineParts, timeStyled)
			}
			if msg.Key.FromMe {
				lineParts = append(lineParts, bodyStyle.Render(" "))
			}
			bodyContent := strings.Join(lineParts, "")
			if msg.Key.FromMe {
				if isMediaMsg {
					bodyContent = lipgloss.NewStyle().Width(max(1, w-2)).Align(lipgloss.Right).Render(bodyContent)
				} else {
					bodyContent = indent + bodyContent
				}
			}
			lastBodyPlainW = runeDisplayWidth(ln)
			if !msg.Key.FromMe && i == 0 {
				lastBodyPlainW += runeDisplayWidth(senderName + ": ")
			}
			block = append(block, bodyContent)
		}
		if reactionLine != "" {
			reactionOffset := max(0, lastBodyPlainW-runeDisplayWidth(reactionLinePlain))
			if msg.Key.FromMe {
				reactionLine = indent + strings.Repeat(" ", reactionOffset) + reactionLine
			} else {
				reactionLine = strings.Repeat(" ", reactionOffset) + reactionLine
			}
			block = append(block, reactionLine)
		}

		if selectedBG != "" && !x.replyPickMode {
			hlStyle := lipgloss.NewStyle().Background(selectedBG)
			for i, ln := range block {
				block[i] = hlStyle.Render(ln)
			}
		}
		timeLine := len(wrapped)
		if hasQuoteLine {
			timeLine++
		}
		msgBlocks = append(msgBlocks, block)
		msgTimestamps = append(msgTimestamps, msg.MessageTimestamp)
		msgBlockMsgs = append(msgBlockMsgs, msg)
		msgBlockTimeLine = append(msgBlockTimeLine, timeLine)

		total := 0
		for _, b := range msgBlocks {
			total += len(b)
		}
		if total >= needed {
			break
		}
	}
	for i, j := 0, len(msgBlocks)-1; i < j; i, j = i+1, j-1 {
		msgBlocks[i], msgBlocks[j] = msgBlocks[j], msgBlocks[i]
		msgTimestamps[i], msgTimestamps[j] = msgTimestamps[j], msgTimestamps[i]
		msgBlockMsgs[i], msgBlockMsgs[j] = msgBlockMsgs[j], msgBlockMsgs[i]
		msgBlockTimeLine[i], msgBlockTimeLine[j] = msgBlockTimeLine[j], msgBlockTimeLine[i]
	}
	all := []string{}
	allDates := []string{}
	allBlockIdx := []int{}
	allTimeLine := []bool{}
	lastDay := ""
	for idx, b := range msgBlocks {
		dayLabel := dateSeparatorLabel(msgTimestamps[idx])
		if dayLabel != lastDay {
			lastDay = dayLabel
			all = append(all, dateSeparatorLine(dayLabel, w))
			allDates = append(allDates, "")
			allBlockIdx = append(allBlockIdx, 0)
			allTimeLine = append(allTimeLine, false)
		}
		timeLineInBlock := msgBlockTimeLine[idx]
		for j, ln := range b {
			all = append(all, ln)
			allDates = append(allDates, dayLabel)
			allBlockIdx = append(allBlockIdx, idx+1)
			allTimeLine = append(allTimeLine, j == timeLineInBlock)
		}
	}
	start := len(all) - h - x.scroll
	if start < 0 {
		start = 0
	}
	end := start + h
	if end > len(all) {
		end = len(all)
	}
	lines := make([]string, end-start)
	copy(lines, all[start:end])
	if len(lines) > 0 && start < len(allDates) {
		pinLabel := ""
		for i := start; i >= 0 && pinLabel == ""; i-- {
			if i < len(allDates) && allDates[i] != "" {
				pinLabel = allDates[i]
			}
		}
		if pinLabel != "" {
			firstIsSep := start < len(allDates) && allDates[start] == ""
			if !firstIsSep {
				lines[0] = dateSeparatorLine(pinLabel, w)
			}
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().
		Width(w).
		Height(h).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}

func (x m) name(c chat) string {
	if strings.HasSuffix(c.ID, "@g.us") {
		if c.Name != "" {
			return c.Name
		}
		if c.Subject != "" {
			return c.Subject
		}
		return num(c.ID)
	}
	if n, ok := x.names[num(c.ID)]; ok && strings.TrimSpace(n) != "" {
		return n
	}
	if n, ok := x.whitelist[num(c.ID)]; ok && strings.TrimSpace(n) != "" {
		return n
	}
	if c.Name != "" {
		return c.Name
	}
	if c.Subject != "" {
		return c.Subject
	}
	if ct, ok := x.contacts[c.ID]; ok {
		if ct.Notify != "" {
			return ct.Notify
		}
		if ct.Name != "" {
			return ct.Name
		}
	}
	n := num(c.ID)
	if ct, ok := x.contactsByNumber[n]; ok {
		if strings.TrimSpace(ct.Notify) != "" {
			return ct.Notify
		}
		if strings.TrimSpace(ct.Name) != "" {
			return ct.Name
		}
	}
	return num(c.ID)
}

func (x m) nameFor(id string) string {
	for _, c := range x.chats {
		if c.ID == id {
			return x.name(c)
		}
	}
	return x.name(chat{ID: id})
}

func (x m) senderIDForMsg(msg wireMsg) string {
	if msg.Key.Participant != "" {
		return msg.Key.Participant
	}
	return msg.Key.RemoteJID
}

func (x m) senderNameForMsg(msg wireMsg) string {
	return x.nameFor(x.senderIDForMsg(msg))
}

func (x m) msgIDAtLine(lineIdx, w, h int) string {
	items := x.msgs[x.active]
	reactionsForLine := map[string]int{}
	for _, msg := range items {
		if rxn, ok := msg.Message["reactionMessage"].(map[string]any); ok {
			if tid, _ := rxn["targetMsgID"].(string); tid != "" {
				reactionsForLine[tid]++
			}
		}
	}
	needed := h + x.scroll
	blocks := [][]string{}
	ids := [][]string{}
	for i := len(items) - 1; i >= 0; i-- {
		msg := items[i]
		if _, ok := msg.Message["reactionMessage"]; ok {
			continue
		}
		mb := renderMessageBody(msg.Message)
		if mb == "" {
			mb = "[media]"
		}
		availableW := chatMessageWrapWidth(w, mb)
		senderName := "Me"
		if !msg.Key.FromMe {
			senderName = truncate(x.senderNameForMsg(msg), 12)
		}
		wrapped := wrapMessageLines(mb, availableW, msg.Key.FromMe, senderName)
		hasQuote := false
		if ext, ok := msg.Message["extendedTextMessage"].(map[string]any); ok {
			if qt, _ := ext["quotedText"].(string); qt != "" {
				hasQuote = true
			}
		}
		lineCount := len(wrapped)
		if hasQuote {
			lineCount++
		}
		if reactionsForLine[msg.Key.ID] > 0 {
			lineCount++
		}
		block := make([]string, lineCount)
		idBlock := make([]string, lineCount)
		for j := range block {
			block[j] = ""
			idBlock[j] = msg.Key.ID
		}
		blocks = append(blocks, block)
		ids = append(ids, idBlock)
		total := 0
		for _, b := range blocks {
			total += len(b)
		}
		if total >= needed {
			break
		}
	}
	for i, j := 0, len(blocks)-1; i < j; i, j = i+1, j-1 {
		blocks[i], blocks[j] = blocks[j], blocks[i]
		ids[i], ids[j] = ids[j], ids[i]
	}
	allIDs := []string{}
	for _, id := range ids {
		allIDs = append(allIDs, id...)
	}
	start := len(allIDs) - h - x.scroll
	if start < 0 {
		start = 0
	}
	visible := allIDs[start:]
	if len(visible) > h {
		visible = visible[:h]
	}
	if lineIdx < 0 || lineIdx >= len(visible) {
		return ""
	}
	return visible[lineIdx]
}

func (x *m) setTopBar(msg string) tea.Cmd {
	x.topBarVer++
	x.topBarMsg = msg
	x.topBarShown = 0
	ver := x.topBarVer
	return tea.Batch(nextTopBarTypeTick(ver), clearTopBarAfter(ver, 2200*time.Millisecond))
}
func nextTopBarTypeTick(ver int) tea.Cmd {
	return tea.Tick(28*time.Millisecond, func(time.Time) tea.Msg { return topBarTypeMsg{ver: ver} })
}
func nextCursorBlink() tea.Cmd {
	return tea.Tick(530*time.Millisecond, func(time.Time) tea.Msg { return cursorBlinkMsg{} })
}
func nextSpinnerTick() tea.Cmd {
	return tea.Tick(170*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}
func clearTopBarAfter(ver int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return topBarClearMsg{ver: ver} })
}
func setTopBarAfter(msg string, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return topBarSetMsg{msg: msg} })
}
func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}
