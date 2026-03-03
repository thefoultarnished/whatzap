package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (x m) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		x.w, x.h = v.Width, v.Height
	case initMsg:
		if v.err != nil {
			x.err = ""
			x.status = "Error: " + v.err.Error()
			return x, nil
		}
		if v.demo {
			x.loadDemoState()
			return x, x.setTopBar("Demo mode: fake chats loaded")
		}
		x.startedBackend, x.backend = v.started, v.cmd
		x.status = "Connecting..."
		return x, tea.Batch(openWS(x.wsURL), postEmpty(x.client, x.baseURL+"/start", nil))
	case wsOpenMsg:
		if v.err != nil {
			x.err = ""
			if x.status == "ready" {
				return x, tea.Batch(x.setTopBar("Connection failed: "+v.err.Error()), tea.Tick(2*time.Second, func(time.Time) tea.Msg { return reconnectMsg{} }))
			}
			x.status = "Error: " + v.err.Error()
			return x, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return reconnectMsg{} })
		}
		x.ws, x.wsCh = v.conn, v.ch
		return x, readWS(x.wsCh)
	case reconnectMsg:
		return x, openWS(x.wsURL)
	case wsEvtMsg:
		if !v.ok {
			return x, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return reconnectMsg{} })
		}
		cmds := []tea.Cmd{readWS(x.wsCh)}
		switch v.evt.Type {
		case "qr":
			var qr string
			_ = json.Unmarshal(v.evt.Payload, &qr)
			x.status = "qr"
			x.qrRaw = qr
		case "ready":
			x.status = "ready"
			x.qrRaw = ""
			cmds = append(cmds, getChats(x.client, x.baseURL), getContacts(x.client, x.baseURL), getWhitelist(x.client, x.baseURL))
		case "chats:loaded":
			cmds = append(cmds, getChats(x.client, x.baseURL))
			if x.active != "" {
				cmds = append(cmds, getMsgs(x.client, x.baseURL, x.active, 120))
			}
		case "contacts:updated":
			cmds = append(cmds, getContacts(x.client, x.baseURL))
		case "message":
			var wm wireMsg
			if err := json.Unmarshal(v.evt.Payload, &wm); err == nil {
				x.msgs[wm.Key.RemoteJID] = append(x.msgs[wm.Key.RemoteJID], wm)
				if !wm.Key.FromMe && wm.Key.ID != "" {
					x.flashUntil[wm.Key.ID] = time.Now().Add(5 * time.Second)
					x.msgActivityUntil = time.Now().Add(3 * time.Second)
					x.msgActivityType = "received"
				}
			}
		case "receipt":
			var rm receiptMsg
			if err := json.Unmarshal(v.evt.Payload, &rm); err == nil {
				if msgs, ok := x.msgs[rm.ChatID]; ok {
					updated := false
					for i := range msgs {
						if !msgs[i].Key.FromMe {
							continue
						}
						for _, id := range rm.MessageIDs {
							if msgs[i].Key.ID == id {
								msgs[i].ReceiptStatus = rm.ReceiptStatus
								updated = true
								break
							}
						}
					}
					if updated {
						x.msgs[rm.ChatID] = msgs
						x.mainCache.result = ""
					}
				}
			}
		case "call":
			var cm callMsg
			if err := json.Unmarshal(v.evt.Payload, &cm); err == nil {
				cmds = append(cmds, x.setTopBar(x.callBanner(cm)))
			}
		}
		return x, tea.Batch(cmds...)
	case dataErr:
		if v.err != nil {
			x.err = ""
			if x.status == "ready" {
				return x, x.setTopBar(v.err.Error())
			}
			x.status = "Error: " + v.err.Error()
		}
	case topBarClearMsg:
		if v.ver == x.topBarVer {
			x.topBarMsg = ""
			x.topBarShown = 0
		}
	case topBarTypeMsg:
		if v.ver == x.topBarVer {
			n := graphemeCount(x.topBarMsg)
			if x.topBarShown < n {
				x.topBarShown++
				return x, nextTopBarTypeTick(v.ver)
			}
		}
	case topBarSetMsg:
		return x, x.setTopBar(v.msg)
	case syncGroupsDoneMsg:
		return x, tea.Batch(x.setTopBar(v.msg), getChats(x.client, x.baseURL))
	case cursorBlinkMsg:
		if time.Since(x.lastTypeTime) < 1*time.Second {
			x.cursorOn = true
		} else {
			x.cursorOn = !x.cursorOn
		}
		x.pulseOn = !x.pulseOn
		now := time.Now()
		for id, until := range x.flashUntil {
			if !until.After(now) {
				delete(x.flashUntil, id)
			}
		}
		return x, nextCursorBlink()
	case spinnerTickMsg:
		x.spinnerFrame = (x.spinnerFrame + 1) % len(spinnerFrames)
		return x, nextSpinnerTick()
	case chatsMsg:
		if v.err != nil {
			x.err = ""
			return x, x.setTopBar(v.err.Error())
		}
		x.chats = v.chats
		sort.Slice(x.chats, func(i, j int) bool { return x.chats[i].ConversationTimestamp > x.chats[j].ConversationTimestamp })
		x.ensureSideVisible(x.sideViewRows())
	case contactsMsg:
		if v.err != nil {
			x.err = ""
			return x, x.setTopBar(v.err.Error())
		}
		x.contacts = map[string]contact{}
		for _, c := range v.contacts {
			x.contacts[c.ID] = c
		}
	case msgsMsg:
		if v.err != nil {
			x.err = ""
			return x, x.setTopBar(v.err.Error())
		}
		x.msgs[v.chatID] = v.msgs
	case sentMsg:
		if v.err != nil {
			x.err = ""
			return x, x.setTopBar(v.err.Error())
		}
		x.msgs[v.chatID] = append(x.msgs[v.chatID], v.msg)
		x.mainCache.result = ""
		x.scroll = 0
		x.msgActivityUntil = time.Now().Add(3 * time.Second)
		x.msgActivityType = "sent"
	case whitelistLoadMsg:
		if v.err != nil {
			x.err = ""
			return x, x.setTopBar(v.err.Error())
		}
		x.whitelist = v.whitelist
		x.names = v.names
	case whitelistSetMsg:
		if v.err != nil {
			x.err = ""
			return x, x.setTopBar(v.err.Error())
		}
	case logoutMsg:
		if v.err != nil {
			x.err = v.err.Error()
			return x, x.setTopBar("Logout failed: " + v.err.Error())
		}
		x.active = ""
		x.chats = nil
		x.msgs = map[string][]wireMsg{}
		x.contacts = map[string]contact{}
		x.whitelist = map[string]string{}
		x.names = map[string]string{}
		x.replyTo = nil
		x.selectedMsgID = ""
		x.status = v.msg
		x.err = ""
		x.mainCache.result = ""
		return x, x.setTopBar(v.msg)
	case tea.MouseMsg:
		if v.Action == tea.MouseActionPress && v.Button == tea.MouseButtonLeft && x.mode == "chat" && x.active != "" {
			sidePad := max(2, x.w/20)
			contentW := x.w - sidePad*2
			leftW := min(28, max(24, contentW/3))
			msgPaneX := sidePad + 1 + leftW + 1
			msgPaneTopY := 3
			msgPaneH := x.h - 6
			if v.X >= msgPaneX && v.Y >= msgPaneTopY && v.Y < msgPaneTopY+msgPaneH {
				lineIdx := v.Y - msgPaneTopY
				rightW := contentW - leftW
				if id := x.msgIDAtLine(lineIdx, rightW, msgPaneH); id != "" {
					isDouble := v.Y == x.lastClickY && time.Since(x.lastClickTime) < 350*time.Millisecond
					x.lastClickY = v.Y
					x.lastClickTime = time.Now()
					if isDouble {
						for i := range x.msgs[x.active] {
							if x.msgs[x.active][i].Key.ID == id {
								cp := x.msgs[x.active][i]
								x.replyTo = &cp
								x.selectedMsgID = ""
								return x, nil
							}
						}
					} else if x.selectedMsgID == id {
						x.selectedMsgID = ""
					} else {
						x.selectedMsgID = id
					}
				}
			}
		}
	case mediaDownloadMsg:
		if v.err != nil {
			return x, x.setTopBar(v.err.Error())
		}
		return x, tea.Batch(x.setTopBar("Opened: "+filepath.Base(v.path)), openFile(v.path))
	case flushInputMsg:
		x.input += x.inputBuf
		x.inputBuf = ""
		x.inputFlushScheduled = false
		return x, nil
	case tea.KeyMsg:
		x.lastTypeTime = time.Now()
		return x.key(v)
	}
	return x, nil
}

func (x m) callBanner(cm callMsg) string {
	name := ""
	switch {
	case cm.GroupID != "":
		name = x.nameFor(cm.GroupID)
	case cm.CallerID != "":
		name = x.nameFor(cm.CallerID)
	}
	if strings.TrimSpace(name) == "" {
		if cm.GroupID != "" {
			name = num(cm.GroupID)
		} else if cm.CallerID != "" {
			name = num(cm.CallerID)
		} else {
			name = "unknown"
		}
	}
	callType := "call"
	if cm.Media != "" {
		callType = cm.Media + " call"
	}
	switch cm.Status {
	case "incoming":
		if cm.GroupID != "" {
			return "Incoming " + callType + " in " + name
		}
		return "Incoming " + callType + " from " + name
	case "ended":
		if cm.Reason != "" {
			return "Call ended: " + name + " (" + cm.Reason + ")"
		}
		return "Call ended: " + name
	default:
		return "Call update: " + name
	}
}

func (x m) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+c":
		return x, tea.Quit
	case "alt+c":
		x.sidebarTab = "chats"
		x.sidebarFocused = true
		if x.mode == "chat" {
			x.mode = "nav"
		}
		x.sel = 0
		x.sideScroll = 0
		x.ensureSideVisible(x.sideViewRows())
		return x, nil
	case "alt+p":
		x.sidebarTab = "contacts"
		x.sidebarFocused = true
		if x.mode == "chat" {
			x.mode = "nav"
		}
		x.sel = 0
		x.sideScroll = 0
		x.ensureSideVisible(x.sideViewRows())
		return x, nil
	case "alt+s":
		if x.status != "ready" {
			return x, nil
		}
		if x.mode == "search" {
			x.sidebarFocused = false
			if x.active != "" {
				x.mode = "chat"
			} else {
				x.mode = "nav"
			}
			x.search = ""
			x.searchInput = ""
			x.sel = 0
			x.ensureSideVisible(x.sideViewRows())
			return x, nil
		}
		x.leftInputFocused = false
		x.sidebarFocused = true
		x.mode = "search"
		x.searchInput = ""
		x.ensureSideVisible(x.sideViewRows())
		return x, nil
	case "alt+m":
		x.mouseEnabled = !x.mouseEnabled
		currentConfig.MouseEnabled = x.mouseEnabled
		saveConfig()
		if x.mouseEnabled {
			return x, tea.Batch(x.setTopBar("Mouse on"), func() tea.Msg { return tea.EnableMouseCellMotion() })
		}
		return x, tea.Batch(x.setTopBar("Mouse off - zoom restored"), func() tea.Msg { return tea.DisableMouse() })
	case "alt+o":
		if x.mode == "chat" && x.active != "" {
			msgs := x.msgs[x.active]
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].MediaProto != "" {
					return x, tea.Batch(
						x.setTopBar("Opening media..."),
						downloadMedia(x.client, x.baseURL, x.active, msgs[i].Key.ID),
					)
				}
			}
			return x, x.setTopBar("No media in this chat")
		}
	case "ctrl+k":
		x.leftInputFocused = true
		x.sidebarFocused = false
		if x.leftInput == "" {
			x.leftInput = "/"
		}
		if x.mode == "search" {
			x.mode = "nav"
		}
		return x, nil
	case "alt+e":
		if x.status == "ready" && x.mode == "chat" && x.active != "" {
			x.openEmojiPicker()
		}
		return x, nil
	}
	if x.leftInputFocused {
		return x.handleLeftInput(k)
	}
	if x.status != "ready" {
		return x, nil
	}
	if x.emojiPickerOpen {
		return x.handleEmojiPicker(k)
	}
	switch x.mode {
	case "nav":
		switch k.String() {
		case "/":
			x.mode, x.search, x.searchInput, x.sel = "search", "", "", 0
			x.sidebarFocused = true
			x.ensureSideVisible(x.sideViewRows())
		case "up":
			if x.sel > 0 {
				x.sel--
			}
			x.ensureSideVisible(x.sideViewRows())
		case "down":
			if x.sel < len(x.filtered())-1 {
				x.sel++
			}
			x.ensureSideVisible(x.sideViewRows())
		case "enter":
			x.sidebarFocused = false
			return x.openSelectedChat()
		case "tab":
			x.sidebarFocused = false
			return x.openSelectedChat()
		}
	case "search":
		switch k.Type {
		case tea.KeyEsc:
			if x.active != "" {
				x.mode = "chat"
			} else {
				x.mode = "nav"
			}
			x.sidebarFocused = false
			x.search, x.searchInput, x.sel = "", "", 0
			x.ensureSideVisible(x.sideViewRows())
		case tea.KeyBackspace:
			if x.searchInput != "" {
				x.searchInput = graphemeDeleteLast(x.searchInput)
				x.search = x.searchInput
				x.ensureSideVisible(x.sideViewRows())
			}
		case tea.KeyUp:
			if x.sel > 0 {
				x.sel--
			}
			x.ensureSideVisible(x.sideViewRows())
		case tea.KeyDown:
			if x.sel < len(x.filtered())-1 {
				x.sel++
			}
			x.ensureSideVisible(x.sideViewRows())
		case tea.KeyEnter:
			q := strings.TrimSpace(x.searchInput)
			if q == "/logout" {
				return x, logout(x.client, x.baseURL)
			}
			x.sidebarFocused = false
			x.search = q
			return x.openSelectedChat()
		default:
			if k.Type == tea.KeyTab {
				x.sidebarFocused = false
				x.search = strings.TrimSpace(x.searchInput)
				return x.openSelectedChat()
			}
			if len(k.Runes) > 0 && k.Type != tea.KeyTab {
				var b strings.Builder
				for _, r := range k.Runes {
					if unicode.IsLetter(r) || unicode.IsNumber(r) {
						b.WriteRune(r)
					}
				}
				if b.Len() > 0 {
					x.searchInput += b.String()
					x.search = x.searchInput
					x.ensureSideVisible(x.sideViewRows())
				}
			}
		}
	case "chat":
		if k.Alt && (k.Type == tea.KeyUp || k.Type == tea.KeyDown) {
			f := x.filtered()
			if len(f) == 0 {
				return x, nil
			}
			cur := x.sel
			if x.active != "" {
				for i, c := range f {
					if c.ID == x.active {
						cur = i
						break
					}
				}
			}
			next := cur
			if k.Type == tea.KeyUp && cur > 0 {
				next = cur - 1
			}
			if k.Type == tea.KeyDown && cur < len(f)-1 {
				next = cur + 1
			}
			if next != cur {
				x.sel = next
				x.sidebarFocused = false
				x.ensureSideVisible(x.sideViewRows())
				return x.openSelectedChat()
			}
			return x, nil
		}
		switch k.Type {
		case tea.KeyTab:
			if !x.sidebarFocused {
				x.input += x.inputBuf
				x.inputBuf = ""
				x.inputFlushScheduled = false
				if completed, ok := completeChatInputStep(x.input); ok {
					x.input = completed
					x.inputAllSelected = false
					return x, nil
				}
			}
			x.sidebarFocused = !x.sidebarFocused
			x.leftInputFocused = false
			return x, nil
		case tea.KeyEsc:
			if x.inputAllSelected {
				x.inputAllSelected = false
				return x, nil
			}
			if strings.HasPrefix(x.input, "@") {
				x.input = ""
				return x, nil
			}
			if x.sidebarFocused {
				x.sidebarFocused = false
				return x, nil
			}
			if x.replyTo != nil {
				x.replyTo = nil
				return x, nil
			}
			x.mode, x.active = "nav", ""
			x.ensureSideVisible(x.sideViewRows())
		case tea.KeyUp:
			if x.sidebarFocused {
				if x.sel > 0 {
					x.sel--
				}
				x.ensureSideVisible(x.sideViewRows())
				return x.openSelectedChat()
			}
			x.scroll++
		case tea.KeyDown:
			if x.sidebarFocused {
				if x.sel < len(x.filtered())-1 {
					x.sel++
				}
				x.ensureSideVisible(x.sideViewRows())
				return x.openSelectedChat()
			}
			if x.scroll > 0 {
				x.scroll--
			}
		case tea.KeyCtrlA:
			x.input += x.inputBuf
			x.inputBuf = ""
			x.inputFlushScheduled = false
			if !x.sidebarFocused && x.input != "" {
				x.inputAllSelected = true
			}
		case tea.KeyBackspace:
			if x.sidebarFocused {
				return x, nil
			}
			x.input += x.inputBuf
			x.inputBuf = ""
			x.inputFlushScheduled = false
			if x.inputAllSelected {
				x.input = ""
				x.inputAllSelected = false
				return x, nil
			}
			if x.input != "" {
				x.input = graphemeDeleteLast(x.input)
			}
		case tea.KeyEnter:
			if x.sidebarFocused {
				x.sidebarFocused = false
				return x.openSelectedChat()
			}
			x.input += x.inputBuf
			x.inputBuf = ""
			x.inputFlushScheduled = false
			txt := strings.TrimSpace(x.input)
			x.input = ""
			if atIdx := atReplyPrefixEnd(txt); atIdx > 0 {
				txt = strings.TrimSpace(txt[atIdx:])
			}
			txt = strings.TrimSpace(sanitizeOutgoingText(txt))
			if !hasVisibleText(txt) || x.active == "" {
				return x, nil
			}
			if cmd, handled := x.handleSlash(txt); handled {
				return x, cmd
			}
			if _, ok := x.whitelist[num(x.active)]; !ok {
				return x, x.setTopBar("Not whitelisted - use /whitelist to enable")
			}
			replyTo := x.replyTo
			x.replyTo = nil
			if x.demoMode {
				return x, demoSend(x.active, txt, replyTo)
			}
			return x, send(x.client, x.baseURL, x.active, txt, replyTo)
		default:
			if x.sidebarFocused {
				return x, nil
			}
			if k.String() == "r" && x.input == "" && x.active != "" {
				if x.selectedMsgID != "" {
					for i := range x.msgs[x.active] {
						if x.msgs[x.active][i].Key.ID == x.selectedMsgID {
							cp := x.msgs[x.active][i]
							x.replyTo = &cp
							x.selectedMsgID = ""
							return x, nil
						}
					}
				}
				msgs := x.msgs[x.active]
				for i := len(msgs) - 1; i >= 0; i-- {
					if !msgs[i].Key.FromMe {
						cp := msgs[i]
						x.replyTo = &cp
						return x, nil
					}
				}
				return x, x.setTopBar("No received messages to reply to")
			}
			if len(k.Runes) > 0 {
				ch := k.Runes[0]
				if x.inputAllSelected {
					x.input = string(k.Runes)
					x.inputBuf = ""
					x.inputFlushScheduled = false
					x.inputAllSelected = false
				} else if ch == ' ' && strings.HasPrefix(x.input, "@") {
					if label, _, ok := parseAtReplyToken(x.input); ok {
						sidePad := max(2, x.w/20)
						contentW := x.w - sidePad*2
						leftW := min(28, max(24, contentW/3))
						rightW := contentW - leftW
						msgH := x.h - 6
						if msg := x.atReplyMsg(rightW, msgH, label); msg != nil {
							x.replyTo = msg
							x.input = ""
							return x, nil
						}
					}
					x.input += " "
				} else if strings.HasPrefix(x.input, "@") {
					x.input += string(k.Runes)
				} else {
					x.inputBuf += string(k.Runes)
					if !x.inputFlushScheduled {
						x.inputFlushScheduled = true
						return x, tea.Tick(2*time.Millisecond, func(time.Time) tea.Msg { return flushInputMsg{} })
					}
				}
			}
		}
	}
	return x, nil
}

func (x *m) handleSlash(txt string) (tea.Cmd, bool) {
	return x.runCommand(txt, false)
}

func (x m) handleEmojiPicker(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := x.emojiVisibleRows()
	switch k.Type {
	case tea.KeyEsc:
		x.closeEmojiPicker()
	case tea.KeyEnter:
		if !x.insertSelectedEmoji() {
			return x, x.setTopBar("No emoji selected")
		}
	case tea.KeyUp:
		x.emojiSel--
		x.ensureEmojiVisible(rows)
	case tea.KeyDown:
		x.emojiSel++
		x.ensureEmojiVisible(rows)
	case tea.KeyPgUp:
		x.emojiSel -= rows
		x.ensureEmojiVisible(rows)
	case tea.KeyPgDown:
		x.emojiSel += rows
		x.ensureEmojiVisible(rows)
	case tea.KeyBackspace:
		if x.emojiQuery != "" {
			x.emojiQuery = graphemeDeleteLast(x.emojiQuery)
			x.emojiResultsDirty = true
			x.emojiSel = 0
			x.emojiScroll = 0
			x.ensureEmojiVisible(rows)
		}
	default:
		if len(k.Runes) > 0 {
			x.emojiQuery += string(k.Runes)
			x.emojiResultsDirty = true
			x.emojiSel = 0
			x.emojiScroll = 0
			x.ensureEmojiVisible(rows)
		}
	}
	return x, nil
}

func (x m) handleLeftInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.Type {
	case tea.KeyEsc:
		x.leftInput = ""
		x.leftInputFocused = false
	case tea.KeyBackspace:
		if x.leftInput != "" {
			x.leftInput = graphemeDeleteLast(x.leftInput)
		}
	case tea.KeyEnter:
		txt := strings.TrimSpace(x.leftInput)
		x.leftInput = ""
		x.leftInputFocused = false
		if txt == "" {
			break
		}
		if cmd, handled := x.handleGlobalCommand(txt); handled {
			return x, cmd
		}
		return x, x.setTopBar("unknown command: " + txt)
	case tea.KeyTab:
		if best := commandBestMatch(x.leftInput); best != "" {
			x.leftInput = best
		} else {
			x.leftInputFocused = false
		}
	default:
		if len(k.Runes) > 0 {
			x.leftInput += string(k.Runes)
		}
	}
	return x, nil
}

func (x *m) handleGlobalCommand(txt string) (tea.Cmd, bool) {
	return x.runCommand(txt, true)
}

func (x *m) runCommand(txt string, includeGlobal bool) (tea.Cmd, bool) {
	switch {
	case includeGlobal && txt == "/exit":
		return tea.Quit, true
	case includeGlobal && txt == "/restart":
		x.restartRequested = true
		return tea.Quit, true
	case txt == "/logout":
		return logout(x.client, x.baseURL), true
	case includeGlobal && txt == "/synccontacts":
		if x.demoMode {
			return x.setTopBar("Demo mode: contacts already fake"), true
		}
		return tea.Batch(x.setTopBar("Syncing contacts..."), syncContacts(x.client, x.baseURL)), true
	case includeGlobal && txt == "/syncgroups":
		if x.demoMode {
			return x.setTopBar("Demo mode: groups already fake"), true
		}
		return tea.Batch(x.setTopBar("Syncing groups..."), syncGroups(x.client, x.baseURL)), true
	case txt == "/whitelistall":
		if len(x.chats) == 0 {
			if includeGlobal {
				return x.setTopBar("No chats loaded yet"), true
			}
			x.err = "no chats loaded yet"
			return nil, true
		}
		added := 0
		cmds := []tea.Cmd{}
		for _, c := range x.chats {
			n := num(c.ID)
			if _, exists := x.whitelist[n]; !exists {
				added++
			}
			name := x.nameFor(c.ID)
			x.whitelist[n] = name
			cmds = append(cmds, setWhitelistEntry(x.client, x.baseURL, n, name, 1))
		}
		msg := fmt.Sprintf("Whitelisted %d chats (%d new)", len(x.whitelist), added)
		if x.demoMode {
			return x.setTopBar(msg), true
		}
		cmds = append(cmds, x.setTopBar(msg))
		return tea.Batch(cmds...), true
	case txt == "/blacklistall":
		count := len(x.whitelist)
		if count == 0 {
			return x.setTopBar("Whitelist already empty"), true
		}
		cmds := []tea.Cmd{}
		for n := range x.whitelist {
			cmds = append(cmds, setWhitelistEntry(x.client, x.baseURL, n, "", 0))
		}
		x.whitelist = map[string]string{}
		msg := fmt.Sprintf("Removed %d from whitelist", count)
		if x.demoMode {
			return x.setTopBar(msg), true
		}
		cmds = append(cmds, x.setTopBar(msg))
		return tea.Batch(cmds...), true
	case txt == "/whitelist":
		if includeGlobal && x.active == "" {
			return x.setTopBar("No active chat"), true
		}
		n := num(x.active)
		_, already := x.whitelist[n]
		name := x.nameFor(x.active)
		x.whitelist[n] = name
		msg := "Added to whitelist"
		if already {
			msg = "Already in whitelist"
		}
		if x.demoMode {
			return x.setTopBar(msg), true
		}
		return tea.Batch(setWhitelistEntry(x.client, x.baseURL, n, name, 1), x.setTopBar(msg)), true
	case txt == "/blacklist":
		if includeGlobal && x.active == "" {
			return x.setTopBar("No active chat"), true
		}
		n := num(x.active)
		_, was := x.whitelist[n]
		delete(x.whitelist, n)
		msg := "Removed from whitelist"
		if !was {
			msg = "Not in whitelist"
		}
		if x.demoMode {
			return x.setTopBar(msg), true
		}
		return tea.Batch(setWhitelistEntry(x.client, x.baseURL, n, "", 0), x.setTopBar(msg)), true
	case strings.HasPrefix(txt, "/rename "):
		name := strings.TrimSpace(strings.TrimPrefix(txt, "/rename "))
		if name == "" {
			return x.setTopBar("usage: /rename <name>"), true
		}
		if includeGlobal && x.active == "" {
			return x.setTopBar("No active chat"), true
		}
		n := num(x.active)
		x.names[n] = name
		if _, ok := x.whitelist[n]; ok {
			x.whitelist[n] = name
		}
		if x.demoMode {
			return x.setTopBar("Renamed"), true
		}
		return tea.Batch(setName(x.client, x.baseURL, n, name), x.setTopBar("Renamed")), true
	case txt == "/rename":
		return x.setTopBar("usage: /rename <name>"), true
	case strings.HasPrefix(txt, "/send "), txt == "/send", strings.HasPrefix(txt, "/sendimage"), strings.HasPrefix(txt, "/sendvideo"), strings.HasPrefix(txt, "/sendfile"):
		cmd, usage, matched := parseMediaSendCommand(txt)
		if !matched {
			break
		}
		if usage != "" {
			return x.setTopBar(usage), true
		}
		if includeGlobal && x.active == "" {
			return x.setTopBar("No active chat"), true
		}
		if _, ok := x.whitelist[num(x.active)]; !ok {
			return x.setTopBar("Not whitelisted - use /whitelist to enable"), true
		}
		if x.demoMode {
			return x.setTopBar("Demo mode: media send disabled"), true
		}
		kind := cmd.kind
		if kind == "" {
			var err error
			kind, err = detectMediaSendKind(cmd.path)
			if err != nil {
				return x.setTopBar(err.Error()), true
			}
		}
		x.replyTo = nil
		return sendFile(x.client, x.baseURL, x.active, kind, cmd.path, cmd.caption), true
	case txt == "/emoji":
		x.openEmojiPicker()
		return nil, true
	case txt == "/theme":
		return x.setTopBar("Themes: /theme1tokyonight /theme2catppuccin /theme3monokai /theme4charcoal /theme5aurora"), true
	case txt == "/theme1tokyonight":
		currentTheme = TokyoNight
		currentConfig.ThemeName = "tokyonight"
		saveConfig()
		rehashStyles()
		x.mainCache.result = ""
		return x.setTopBar("Theme: Tokyo Night"), true
	case txt == "/theme2catppuccin":
		currentTheme = Catppuccin
		currentConfig.ThemeName = "catppuccin"
		saveConfig()
		rehashStyles()
		x.mainCache.result = ""
		return x.setTopBar("Theme: Catppuccin"), true
	case txt == "/theme3monokai":
		currentTheme = Monokai
		currentConfig.ThemeName = "monokai"
		saveConfig()
		rehashStyles()
		x.mainCache.result = ""
		return x.setTopBar("Theme: Monokai"), true
	case txt == "/theme4charcoal":
		currentTheme = Charcoal
		currentConfig.ThemeName = "charcoal"
		saveConfig()
		rehashStyles()
		x.mainCache.result = ""
		return x.setTopBar("Theme: Charcoal"), true
	case txt == "/theme5aurora":
		currentTheme = Aurora
		currentConfig.ThemeName = "aurora"
		saveConfig()
		rehashStyles()
		x.mainCache.result = ""
		return x.setTopBar("Theme: Aurora"), true
	case txt == "/mouseon":
		x.mouseEnabled = true
		currentConfig.MouseEnabled = true
		saveConfig()
		return x.setTopBar("Mouse: Enabled"), true
	case txt == "/mouseoff":
		x.mouseEnabled = false
		currentConfig.MouseEnabled = false
		saveConfig()
		return x.setTopBar("Mouse: Disabled"), true
	case strings.HasPrefix(txt, "/"):
		return x.setTopBar("unknown command: " + txt), true
	}
	return nil, false
}

func rehashStyles() {
	brand = lipgloss.Color(currentTheme.Brand)
	accent = lipgloss.Color(currentTheme.Accent)
	purple = lipgloss.Color(currentTheme.Purple)
	amber = lipgloss.Color(currentTheme.Amber)
	red = lipgloss.Color(currentTheme.Red)
	muted = lipgloss.Color(currentTheme.Muted)
	text = lipgloss.Color(currentTheme.Text)
	imageTag = lipgloss.Color(currentTheme.ImageTag)
	videoTag = lipgloss.Color(currentTheme.VideoTag)
	audioTag = lipgloss.Color(currentTheme.AudioTag)
	fileTag = lipgloss.Color(currentTheme.FileTag)
	stickerTag = lipgloss.Color(currentTheme.StickerTag)
	contactTag = lipgloss.Color(currentTheme.ContactTag)
	pollTag = lipgloss.Color(currentTheme.PollTag)
	locationTag = lipgloss.Color(currentTheme.LocationTag)
	anomalyTag = lipgloss.Color(currentTheme.AnomalyTag)
	sentText = lipgloss.Color(currentTheme.SentText)
	receivedText = lipgloss.Color(currentTheme.ReceivedText)
	sentName = lipgloss.Color(currentTheme.SentName)
	receivedName = lipgloss.Color(currentTheme.ReceivedName)
	badgeInk = lipgloss.Color(currentTheme.BadgeInk)
	buttonInk = lipgloss.Color(currentTheme.ButtonInk)
	tagInk = lipgloss.Color(currentTheme.TagInk)
	cursorColor = lipgloss.Color(currentTheme.Cursor)
	qrLight = lipgloss.Color(currentTheme.QRLight)
	qrDark = lipgloss.Color(currentTheme.QRDark)
	shortcutActive = lipgloss.Color(currentTheme.ShortcutActive)
	sidebarActiveBg = lipgloss.Color(currentTheme.SidebarActiveBg)
	sidebarActiveUnreadBg = lipgloss.Color(currentTheme.SidebarActiveUnreadBg)
	replyPreviewBg = lipgloss.Color(currentTheme.ReplyPreviewBg)
	messageSelectedBg = lipgloss.Color(currentTheme.MessageSelectedBg)
	mediaTokenBg = lipgloss.Color(currentTheme.MediaTokenBg)
	mediaTokenPulseBg = lipgloss.Color(currentTheme.MediaTokenPulseBg)

	baseBoxStyle = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(brand).
		Foreground(text).
		Padding(1, 2).
		Align(lipgloss.Center, lipgloss.Center)

	logoStyle = lipgloss.NewStyle().Bold(true).Foreground(brand)
	accentStyle = lipgloss.NewStyle().Foreground(accent).Bold(true)
	mutedStyle = lipgloss.NewStyle().Foreground(muted)
	amberStyle = lipgloss.NewStyle().Foreground(amber).Bold(true)
	purpleStyle = lipgloss.NewStyle().Foreground(purple).Bold(true)
	redStyle = lipgloss.NewStyle().Foreground(red).Bold(true)

	cmdBadgeStyle = lipgloss.NewStyle().Foreground(badgeInk).Background(accent).Bold(true)
	ghostStyle = lipgloss.NewStyle().Foreground(muted)
	cursorStyle = lipgloss.NewStyle().Foreground(cursorColor).Background(cursorColor)

	sidebarStyle = lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(muted)

	msgPaneStyle = lipgloss.NewStyle().Padding(0, 1)
	dateSepStyle = lipgloss.NewStyle().Foreground(muted).Bold(true)

	bColor = qrDark
	wColor = qrLight
	bb = lipgloss.NewStyle().Foreground(bColor).Background(bColor).Render("▀")
	bw = lipgloss.NewStyle().Foreground(bColor).Background(wColor).Render("▀")
	wb = lipgloss.NewStyle().Foreground(wColor).Background(bColor).Render("▀")
	ww = lipgloss.NewStyle().Foreground(wColor).Background(wColor).Render("▀")

	imageTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(imageTag).Bold(true)
	videoTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(videoTag).Bold(true)
	audioTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(audioTag).Bold(true)
	fileTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(fileTag).Bold(true)
	stickerTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(stickerTag).Bold(true)
	contactTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(contactTag).Bold(true)
	pollTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(pollTag).Bold(true)
	locationTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(locationTag).Bold(true)
	anomalyTagStyle = lipgloss.NewStyle().Foreground(tagInk).Background(anomalyTag).Bold(true)
}

func (x m) sidebarItems() []chat {
	if x.sidebarTab == "contacts" {
		out := make([]chat, 0, len(x.contacts))
		for _, ct := range x.contacts {
			if ct.ID == "" || strings.HasSuffix(ct.ID, "@g.us") || ct.ID == "status@broadcast" {
				continue
			}
			out = append(out, chat{ID: ct.ID, Name: ct.Notify, Subject: ct.Name})
		}
		sort.Slice(out, func(i, j int) bool {
			li := strings.ToLower(x.name(out[i]))
			lj := strings.ToLower(x.name(out[j]))
			if li == lj {
				return out[i].ID < out[j].ID
			}
			return li < lj
		})
		return out
	}
	out := make([]chat, 0, len(x.chats))
	for _, ch := range x.chats {
		if ch.ID == "" || ch.ID == "status@broadcast" {
			continue
		}
		if ch.ConversationTimestamp == 0 && len(x.msgs[ch.ID]) == 0 {
			continue
		}
		out = append(out, ch)
	}
	return out
}

func (x m) filtered() []chat {
	items := x.sidebarItems()
	if strings.TrimSpace(x.search) == "" {
		return items
	}
	q := strings.ToLower(strings.TrimSpace(x.search))
	out := make([]chat, 0, len(items))
	for _, c := range items {
		if strings.Contains(strings.ToLower(x.name(c)), q) || strings.Contains(strings.ToLower(c.ID), q) {
			out = append(out, c)
		}
	}
	return out
}
func (x *m) ensureSideVisible(viewRows int) {
	f := x.filtered()
	if len(f) == 0 {
		x.sel = 0
		x.sideScroll = 0
		return
	}
	if x.sel < 0 {
		x.sel = 0
	}
	if x.sel >= len(f) {
		x.sel = len(f) - 1
	}
	if viewRows <= 0 {
		return
	}
	maxStart := max(0, len(f)-viewRows)
	if x.sideScroll > maxStart {
		x.sideScroll = maxStart
	}
	if x.sideScroll < 0 {
		x.sideScroll = 0
	}
	if x.sel < x.sideScroll {
		x.sideScroll = x.sel
	}
	if x.sel >= x.sideScroll+viewRows {
		x.sideScroll = x.sel - viewRows + 1
	}
}
func (x m) sideViewRows() int {
	outerH := x.h - 2
	if outerH <= 0 {
		return 1
	}
	sideH := outerH - 5
	return max(1, sideH-3)
}
func (x m) openSelectedChat() (tea.Model, tea.Cmd) {
	f := x.filtered()
	if len(f) == 0 {
		return x, nil
	}
	if x.sel < 0 || x.sel >= len(f) {
		x.sel = 0
	}
	x.active, x.mode, x.scroll = f[x.sel].ID, "chat", 0

	// Clear the unread dot right away.
	for i := range x.chats {
		if x.chats[i].ID == x.active {
			x.chats[i].UnreadCount = 0
			break
		}
	}

	if x.demoMode {
		return x, nil
	}

	return x, tea.Batch(
		getMsgs(x.client, x.baseURL, x.active, 120),
		postJSON(x.client, x.baseURL+"/messages/read", map[string]string{"chatId": x.active}, func([]byte) tea.Msg { return dataErr{} }),
	)
}
