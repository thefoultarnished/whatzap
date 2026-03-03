package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (x m) View() string {
	if x.w == 0 || x.h == 0 {
		return "loading..."
	}
	frameW := max(1, x.w-2)
	if x.status != "ready" {
		// Keep the status box inside the viewport.
		statusInnerH := max(3, x.h-6)
		statusBody := x.status
		title := logoStyle.Render("WhatZap")
		subtitle := mutedStyle.Render("Terminal WhatsApp client")
		hint := mutedStyle.Render("No browser. Local session.")
		if x.status == "qr" {
			if x.qrRaw != "" {
				maxQRW := max(12, frameW-8)
				maxQRH := max(6, statusInnerH-11)
				statusBody = renderQR(x.qrRaw, maxQRW, maxQRH)
				if statusBody == "" {
					statusBody = x.qrRaw
				}
				statusBody = mutedStyle.Render("[ waiting for scan ]") + "\n\n" + statusBody
				hint = mutedStyle.Render("WhatsApp > Linked Devices > Link a Device")
			} else {
				statusBody = "Generating QR..."
				hint = mutedStyle.Render("Preparing login QR")
			}
		} else if strings.HasPrefix(strings.ToLower(x.status), "logged out") {
			statusBody = accentStyle.Copy().Bold(false).Render(x.status)
			hint = mutedStyle.Render("Restart and scan a QR code to sign in again")
		} else {
			statusBody = accentStyle.Copy().Bold(false).Render("[" + spinnerFrames[x.spinnerFrame] + "] " + statusBody)
		}
		box := baseBoxStyle.Copy().
			Width(frameW).
			Height(statusInnerH)
		msg := title + "\n" + subtitle + "\n\n" + statusBody + "\n\n" + hint
		return lipgloss.Place(x.w, x.h, lipgloss.Center, lipgloss.Center, box.Render(msg))
	}
	outerW := frameW
	outerH := x.h - 2
	contentW := outerW
	leftW := min(28, max(24, contentW/3))
	rightW := contentW - leftW
	head := x.renderHeaderContainer(contentW, leftW)
	side := x.renderSide(leftW, outerH-4)

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
	atInput := ""
	if strings.HasPrefix(x.input, "@") {
		atInput = x.input
	}
	cacheKey := mainCacheKey{
		active:       x.active,
		themeName:    currentConfig.ThemeName,
		msgCount:     len(x.msgs[x.active]),
		lastMsgID:    lastMsgID,
		scroll:       x.scroll,
		w:            x.w,
		h:            x.h,
		atInput:      atInput,
		replyToID:    replyToID,
		selectedMsg:  x.selectedMsgID,
		contactCount: len(x.contacts) + len(x.names),
	}
	var main string
	if x.emojiPickerOpen {
		main = x.renderEmojiPickerPane(rightW, outerH-4)
	} else if !hasFlash && x.mainCache.result != "" && x.mainCache.key == cacheKey {
		main = x.mainCache.result
	} else {
		main = x.renderMain(rightW, outerH-4)
		if !hasFlash {
			x.mainCache.key = cacheKey
			x.mainCache.result = main
		}
	}

	center := lipgloss.JoinHorizontal(lipgloss.Top, side, main)
	typedInput := x.input + x.inputBuf
	splitInputBar := lipgloss.JoinHorizontal(lipgloss.Top, x.renderCommandBox(leftW), x.renderChatInput(rightW, typedInput))

	bodyParts := []string{center}
	if replyBar := x.renderReplyBar(contentW, rightW, outerH-4, typedInput); replyBar != "" {
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
		statusPart = mutedStyle.Render("[") + wifi + connText + mutedStyle.Render("]") + " "
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
		chatName := accentStyle.Render(truncate(x.nameFor(x.active), 22))
		centerContent = " " + chatName
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
	inputGhost := ""
	if rightFocused && !x.inputAllSelected {
		inputGhost = chatInputGhost(typedInput)
	}

	showSend := hasVisibleText(typedInput) && !isCmd && rightFocused
	sendBadge := ""
	sendBadgeW := 0
	if showSend {
		sendBadge = lipgloss.NewStyle().Foreground(buttonInk).Background(brand).Bold(true).Render(" SEND ")
		sendBadgeW = lipgloss.Width(sendBadge)
	}

	textAreaW := max(1, rightW-1-sendBadgeW)
	var inputDisplay string
	if x.inputAllSelected {
		inputDisplay = lipgloss.NewStyle().Foreground(text).Render("") +
			lipgloss.NewStyle().Foreground(buttonInk).Background(accent).Render(typedInput)
	} else {
		inputDisplay = lipgloss.NewStyle().Foreground(text).Render(typedInput)
		if rightFocused && inputGhost != "" {
			firstGhost, restGhost := graphemeSplitFirst(inputGhost)
			if x.cursorOn {
				inputDisplay += cursorStyle.Render(firstGhost)
			} else {
				inputDisplay += ghostStyle.Render(firstGhost)
			}
			inputDisplay += ghostStyle.Render(restGhost)
		} else if rightFocused && x.cursorOn {
			inputDisplay += lipgloss.NewStyle().Foreground(accent).Render("|")
		} else if rightFocused {
			inputDisplay += " "
		}
	}

	if isCmd {
		inputDisplay += lipgloss.NewStyle().Foreground(accent).Bold(true).Render("  CMD")
	}
	if x.mode == "nav" || (x.mode == "chat" && x.sidebarFocused) {
		inputDisplay = lipgloss.NewStyle().Foreground(muted).Render("  Ctrl+K for commands | Tab to focus")
	} else if rightFocused && !x.emojiPickerOpen && typedInput == "" {
		inputDisplay = lipgloss.NewStyle().Foreground(muted).Render("  Type a message | Alt+E for emoji")
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

func (x m) renderReplyBar(contentW, rightW, mainH int, typedInput string) string {
	var displayReplyTo *wireMsg
	atCancelHint := "  | Esc to cancel"
	if strings.HasPrefix(typedInput, "@") {
		if label, _, ok := parseAtReplyToken(typedInput); ok {
			if msg := x.atReplyMsg(rightW, mainH, label); msg != nil {
				displayReplyTo = msg
				atCancelHint = "  | Space to confirm, Esc to cancel"
			}
		}
	}
	if displayReplyTo == nil {
		displayReplyTo = x.replyTo
	}
	if displayReplyTo == nil {
		return ""
	}

	rSender := x.senderNameForMsg(*displayReplyTo)
	rText := renderMessageBody(displayReplyTo.Message)
	if len([]rune(rText)) > 60 {
		rText = string([]rune(rText)[:60]) + "..."
	}
	return lipgloss.NewStyle().
		Width(contentW).
		Foreground(muted).
		Render(lipgloss.NewStyle().Foreground(accent).Render("  -> ") +
			lipgloss.NewStyle().Foreground(accent).Bold(true).Render(rSender+": ") +
			lipgloss.NewStyle().Foreground(muted).Render(rText) +
			lipgloss.NewStyle().Foreground(muted).Render(atCancelHint))
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
	searchIcon := mutedStyle.Render("⌕ ")
	searchLine := searchIcon
	if searchValue == "" && !searchFocused {
		searchLine += mutedStyle.Render("search [Alt+S]")
	} else if searchValue != "" {
		searchLine += lipgloss.NewStyle().Foreground(text).Render(searchValue)
	}
	if searchFocused {
		if x.cursorOn {
			searchLine += lipgloss.NewStyle().Foreground(accent).Render("|")
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
		markerStyle := lipgloss.NewStyle().Foreground(muted)
		if highlighted {
			bg := brand
			if !whitelisted {
				bg = amber
			}
			rowBase = rowBase.Background(bg).Foreground(buttonInk).Bold(isSel && navActive)
			markerStyle = markerStyle.Background(bg).Foreground(buttonInk)
		} else if isActive {
			activeBg := sidebarActiveBg
			if !whitelisted {
				activeBg = sidebarActiveUnreadBg
			}
			rowBase = rowBase.Background(activeBg).Foreground(accent)
			markerStyle = markerStyle.Background(activeBg).Foreground(accent)
		}

		marker := "  "
		if isActive {
			marker = "| "
		}
		rowWidth := max(1, w-2)
		contentWidth := rowWidth - 1
		if contentWidth < 1 {
			contentWidth = 1
		}
		nameWidth := contentWidth - 2
		if nameWidth < 1 {
			nameWidth = 1
		}

		nameText := fmt.Sprintf("%d. %s", i+1, truncate(x.name(c), max(8, w-10)))
		nameText = truncate(nameText, nameWidth)
		line := markerStyle.Render(marker) + rowBase.Render(padRight(nameText, nameWidth))
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

func outgoingMessageIndent(paneW int, blockW int, fromMe bool) string {
	if !fromMe {
		return ""
	}
	paneW = max(1, paneW)
	blockW = max(1, min(blockW, paneW))
	return strings.Repeat(" ", max(0, paneW-blockW))
}

func (x m) renderMain(w, h int) string {
	if x.active == "" {
		return lipgloss.NewStyle().
			Width(w).
			Height(h).
			Padding(1, 2).
			Foreground(muted).
			Render("Select a chat and press Enter")
	}
	items := x.msgs[x.active]
	typedInput := x.input + x.inputBuf
	previewReplyID := ""
	if label, _, ok := parseAtReplyToken(typedInput); ok {
		if msg := x.atReplyMsg(w, h, label); msg != nil {
			previewReplyID = msg.Key.ID
		}
	}
	reactionsFor := map[string][]string{}
	for _, msg := range items {
		if rxn, ok := msg.Message["reactionMessage"].(map[string]any); ok {
			targetID, _ := rxn["targetMsgID"].(string)
			emoji, _ := rxn["emoji"].(string)
			if targetID != "" && emoji != "" {
				rSender := "Me"
				if !msg.Key.FromMe {
					rSender = truncate(x.senderNameForMsg(msg), 10)
				}
				reactionsFor[targetID] = append(reactionsFor[targetID], emoji+" "+rSender)
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

		wrapped := strings.Split(wrapText(msgBody, availableW), "\n")

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
		isReplyPreviewSelected := previewReplyID != "" && msg.Key.ID == previewReplyID
		selectedBG := lipgloss.Color("")
		if isReplyPreviewSelected {
			selectedBG = replyPreviewBg
		} else if isClickedSelected {
			selectedBG = messageSelectedBg
		}
		applySelectedBG := func(st lipgloss.Style) lipgloss.Style {
			if selectedBG != "" {
				return st.Background(selectedBG)
			}
			return st
		}
		timeStyled := applySelectedBG(mutedStyle.Copy().Foreground(timeColor)).Render("  " + timeStr)
		if receiptText != "" {
			receiptColor := muted
			if msg.ReceiptStatus == "read" || msg.ReceiptStatus == "played" {
				receiptColor = accent
			}
			timeStyled += applySelectedBG(mutedStyle.Copy().Foreground(receiptColor).Bold(true)).Render(receiptText)
		}
		block := []string{}
		hasQuoteLine := false

		reactionStr := ""
		if rxns, ok := reactionsFor[msg.Key.ID]; ok && len(rxns) > 0 {
			names := map[string][]string{}
			order := []string{}
			for _, r := range rxns {
				parts := strings.SplitN(r, " ", 2)
				emoji := parts[0]
				name := ""
				if len(parts) > 1 {
					name = parts[1]
				}
				if len(names[emoji]) == 0 {
					order = append(order, emoji)
				}
				names[emoji] = append(names[emoji], name)
			}
			pills := []string{}
			for _, e := range order {
				pills = append(pills, e+" "+strings.Join(names[e], ", "))
			}
			reactionStr = applySelectedBG(mutedStyle).Render("[") +
				applySelectedBG(amberStyle).Render(strings.Join(pills, "  ")) +
				applySelectedBG(mutedStyle).Render("]")
		}

		quotePlain := ""
		if ext, ok := msg.Message["extendedTextMessage"].(map[string]any); ok {
			if qText, _ := ext["quotedText"].(string); qText != "" {
				hasQuoteLine = true
				qParticipant, _ := ext["quotedParticipant"].(string)
				qSender := x.nameFor(qParticipant)
				if qSender == "" {
					qSender = num(qParticipant)
				}
				qText = strings.ReplaceAll(qText, "\n", " ")
				if len([]rune(qText)) > 50 {
					qText = string([]rune(qText)[:50]) + "..."
				}
				quoteText := "  ╭─ " +
					lipgloss.NewStyle().Foreground(muted).Bold(true).Render(qSender+": ") +
					lipgloss.NewStyle().Foreground(muted).Italic(true).Render(qText)
				quotePlain = "  ╭─ " + qSender + ": " + qText
				quoteLine := applySelectedBG(lipgloss.NewStyle().Foreground(muted)).Render(quoteText)
				block = append(block, quoteLine)
			}
		}
		outgoingBlockW := 0
		if msg.Key.FromMe {
			if quotePlain != "" {
				outgoingBlockW = max(outgoingBlockW, runeDisplayWidth(quotePlain))
			}
			for i, ln := range wrapped {
				lineMeasure := ln
				if i == len(wrapped)-1 {
					if reactionStr != "" {
						lineMeasure += " " + strings.Join(strings.Fields(strings.ReplaceAll(reactionStr, "[", " [ ")), "")
					}
					lineMeasure += "  " + timeStr + receiptText + " "
				} else {
					lineMeasure += " "
				}
				outgoingBlockW = max(outgoingBlockW, runeDisplayWidth(lineMeasure))
			}
			outgoingBlockW = min(outgoingBlockW, max(1, w-2))
		}
		indent := outgoingMessageIndent(max(1, w-2), outgoingBlockW, msg.Key.FromMe)
		for i, ln := range wrapped {
			bodyLine := ln
			if isMediaMsg && strings.HasPrefix(ln, "[") {
				if end := strings.Index(ln, "]"); end > 0 {
					token := ln[:end+1]
					rest := ln[end+1:]
					tokenStyle := applySelectedBG(lipgloss.NewStyle().
						Foreground(tagInk).
						Background(mediaTokenBG).
						Bold(true))
					bodyLine = tokenStyle.Render(token) + rest
				}
			}
			if !msg.Key.FromMe && i == 0 {
				namePrefix := applySelectedBG(lipgloss.NewStyle().
					Foreground(receivedName).
					Bold(true)).
					Render(senderName + ": ")
				bodyLine = namePrefix + bodyLine
			}
			if i == len(wrapped)-1 {
				if reactionStr != "" {
					bodyLine += " " + reactionStr
				}
				bodyLine += timeStyled
			}
			if msg.Key.FromMe {
				bodyLine += " "
			}
			bodyStyled := applySelectedBG(lipgloss.NewStyle().Foreground(bodyColor).Bold(isFlashing)).Render(bodyLine)
			if msg.Key.FromMe {
				if isMediaMsg {
					bodyStyled = applySelectedBG(lipgloss.NewStyle().Width(max(1, w-2)).Align(lipgloss.Right).Foreground(bodyColor).Bold(isFlashing)).Render(bodyLine)
				} else {
					bodyStyled = indent + bodyStyled
				}
			}
			block = append(block, bodyStyled)
		}

		if selectedBG != "" {
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
		pinnedFirst := false
		if pinLabel != "" {
			firstIsSep := start < len(allDates) && allDates[start] == ""
			if !firstIsSep {
				lines[0] = dateSeparatorLine(pinLabel, w)
				pinnedFirst = true
			}
		}
		atMode := strings.HasPrefix(typedInput, "@")
		if atMode && len(msgBlockMsgs) > 0 {
			visBlockIdx := allBlockIdx[start:end]
			visTimeLine := allTimeLine[start:end]
			blockToLabel := map[int]string{}
			rCount, sCount, mCount := 0, 0, 0
			for i, bi := range visBlockIdx {
				if i == 0 && pinnedFirst {
					continue
				}
				if bi <= 0 {
					continue
				}
				if !visTimeLine[i] {
					continue
				}
				if _, ok := blockToLabel[bi]; ok {
					continue
				}
				msg := msgBlockMsgs[bi-1]
				switch {
				case isMediaWire(msg):
					mCount++
					blockToLabel[bi] = fmt.Sprintf("M%d", mCount)
				case msg.Key.FromMe:
					sCount++
					blockToLabel[bi] = fmt.Sprintf("S%d", sCount)
				default:
					rCount++
					blockToLabel[bi] = fmt.Sprintf("R%d", rCount)
				}
			}
			for i, bi := range visBlockIdx {
				if i >= len(lines) {
					break
				}
				if i == 0 && pinnedFirst {
					continue
				}
				if bi > 0 && visTimeLine[i] {
					if label, ok := blockToLabel[bi]; ok {
						tag := mutedStyle.Copy().Bold(true).Render("  " + label)
						lines[i] += tag
					}
				}
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
	for _, ct := range x.contacts {
		if num(ct.ID) != n {
			continue
		}
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
	reactionsForLine := map[string]bool{}
	for _, msg := range items {
		if rxn, ok := msg.Message["reactionMessage"].(map[string]any); ok {
			if tid, _ := rxn["targetMsgID"].(string); tid != "" {
				reactionsForLine[tid] = true
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
		wrapped := strings.Split(wrapText(mb, availableW), "\n")
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

func (x m) atReplyMsg(w, h int, label string) *wireMsg {
	if x.active == "" || label == "" {
		return nil
	}
	items := x.msgs[x.active]
	reactionsFor := map[string]bool{}
	for _, msg := range items {
		if rxn, ok := msg.Message["reactionMessage"].(map[string]any); ok {
			if tid, _ := rxn["targetMsgID"].(string); tid != "" {
				reactionsFor[tid] = true
			}
		}
	}
	type entry struct {
		msg      wireMsg
		lines    int
		timeLine int
	}
	needed := h + x.scroll
	entries := []entry{}
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
		wrapped := strings.Split(wrapText(mb, availableW), "\n")
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
		timeLine := len(wrapped)
		if hasQuote {
			timeLine++
		}
		entries = append(entries, entry{msg, lineCount, timeLine})
		total := 0
		for _, e := range entries {
			total += e.lines
		}
		if total >= needed {
			break
		}
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	allBlockIdx := []int{}
	allDates := []string{}
	allTimeLine := []bool{}
	lastDay := ""
	for i, e := range entries {
		dayLabel := dateSeparatorLabel(e.msg.MessageTimestamp)
		if dayLabel != lastDay {
			lastDay = dayLabel
			allBlockIdx = append(allBlockIdx, 0)
			allDates = append(allDates, "")
			allTimeLine = append(allTimeLine, false)
		}
		for k := 0; k < e.lines; k++ {
			allBlockIdx = append(allBlockIdx, i+1)
			allDates = append(allDates, dayLabel)
			allTimeLine = append(allTimeLine, k == e.timeLine)
		}
	}
	if len(allBlockIdx) == 0 {
		return nil
	}
	start := len(allBlockIdx) - h - x.scroll
	if start < 0 {
		start = 0
	}
	end := start + h
	if end > len(allBlockIdx) {
		end = len(allBlockIdx)
	}
	visBlockIdx := allBlockIdx[start:end]
	visTimeLine := allTimeLine[start:end]
	pinnedFirst := false
	if len(visBlockIdx) > 0 && start < len(allDates) {
		pinLabel := ""
		for i := start; i >= 0 && pinLabel == ""; i-- {
			if allDates[i] != "" {
				pinLabel = allDates[i]
			}
		}
		if pinLabel != "" {
			firstIsSep := allDates[start] == ""
			if !firstIsSep {
				pinnedFirst = true
			}
		}
	}
	label = strings.ToUpper(strings.TrimSpace(label))
	seen := map[int]bool{}
	order := []int{}
	for i, bi := range visBlockIdx {
		if i == 0 && pinnedFirst {
			continue
		}
		if bi <= 0 {
			continue
		}
		if !visTimeLine[i] {
			continue
		}
		if !seen[bi] {
			seen[bi] = true
			order = append(order, bi)
		}
	}
	mCount, sCount, rCount := 0, 0, 0
	for _, bi := range order {
		msg := entries[bi-1].msg
		var curr string
		switch {
		case isMediaWire(msg):
			mCount++
			curr = fmt.Sprintf("M%d", mCount)
		case msg.Key.FromMe:
			sCount++
			curr = fmt.Sprintf("S%d", sCount)
		default:
			rCount++
			curr = fmt.Sprintf("R%d", rCount)
		}
		if curr == label {
			cp := msg
			return &cp
		}
	}
	return nil
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
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
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
