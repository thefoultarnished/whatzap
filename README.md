# WhatZAP - WhatsApp but Zappy

## WhatsApp in your terminal. No browser, no bloat

> [!CAUTION]
> **Work in Progress:** This project is under active development. Expect bugs and breaking changes. It uses a local WhatsApp Web session and local caches/state; audit it before trusting it with anything sensitive.
> **Not affiliated with Meta or WhatsApp:** This is an independent project and is not endorsed by, sponsored by, or associated with Meta or WhatsApp.
> **Use at your own risk:** If your account is limited, flagged, blocked, or otherwise affected while using this project, that is your responsibility.

A terminal-first WhatsApp client built in Go. It runs a local `whatsmeow` backend and a Bubble Tea / Lip Gloss TUI on top of it, so you get a native terminal workflow instead of a hidden browser tab.

![WhatzApp CLI](assets/screenshot.png)

## Current Features

- **Direct WhatsApp Web session:** Uses `whatsmeow` instead of a browser wrapper.
- **Go-native terminal UI:** Bubble Tea + Lip Gloss interface with keyboard-first navigation and optional mouse support.
- **Chat list and message history:** Loads chats, contacts, and cached messages into the TUI.
- **Send text and media:** Supports normal text messages plus file/image/video/document sending.
- **Reply support:** Quote and reply to received messages from the chat view.
- **Emoji picker:** In-app emoji selection with search, so emoji input does not depend on the Windows emoji picker path.
- **Live receipts and updates:** Incoming messages, receipt updates, and chat refreshes stream over WebSocket.
- **Whitelist gating:** Sending is restricted to explicitly whitelisted contacts/chats.
- **Contact naming tools:** Rename contacts locally and keep custom display names in the UI.
- **Contact and group sync commands:** Manual sync for contacts and group metadata.
- **Call detection:** Live incoming / ended call notifications are surfaced in the TUI. This is detection only, not answering calls.
- **Offline local state:** Chats, messages, permissions, names, and session/auth state are stored locally so startup is fast.

## Screenshots

![WhatzApp CLI](assets/screenshot.png)

## Architecture

The project has two cooperating processes/components:

1. **Backend**
   File: `backend/main.go`

   Responsibilities:
   - Starts and maintains the WhatsApp Web client through `whatsmeow`
   - Handles QR login/session startup
   - Maintains local state for chats, contacts, messages, whitelist entries, and custom names
   - Persists local state and session/cache data
   - Exposes a local HTTP + WebSocket API
   - Translates WhatsApp events into lightweight wire events for the TUI

2. **TUI frontend**
   Entry point: `backend/tui/main.go`

   Responsibilities:
   - Connects to the local backend over HTTP and WebSocket
   - Renders chats, messages, reply previews, receipts, and notifications
   - Handles keyboard/mouse navigation
   - Sends text/media commands back to the backend
   - Provides command mode, emoji picker flow, and call/status notifications

## Backend Surface

The backend currently exposes local endpoints for:

- `/health`
- `/ws`
- `/start`
- `/chats`
- `/contacts`
- `/messages`
- `/messages/send`
- `/messages/send-file`
- `/messages/read`
- `/sync/contacts`
- `/sync/groups`
- `/whitelist`
- `/whitelist/set`
- `/names/set`
- `/profile-picture`
- `/media/download`
- `/logout`

The WebSocket stream carries live events such as:

- QR/login readiness
- chat refreshes
- contact updates
- incoming messages
- receipt updates
- live call detection events

## TUI Interaction Model

The TUI is split into a left command/sidebar area and a right chat area.

- **Sidebar:** chats and contacts navigation
- **Command box:** slash-command entry
- **Chat pane:** messages, reactions, replies, receipts, and media markers
- **Compose box:** text input, send state, and emoji insertion

Current notable flows:

- `Ctrl+K` opens command input
- `Alt+S` focuses search
- `Alt+E` opens the emoji picker from the chat compose area
- `r` replies to the selected/latest received message
- `/emoji` opens the emoji picker from command/chat command flow

## Supported Commands

Commands currently implemented in the TUI include:

- `/synccontacts` refreshes contacts from WhatsApp and merges local contact metadata.
- `/syncgroups` refreshes group/chat metadata from WhatsApp.
- `/whitelist` allows sending to the currently active chat.
- `/whitelistall` whitelists every currently loaded chat.
- `/blacklist` removes the currently active chat from the send whitelist.
- `/blacklistall` clears the whitelist entirely.
- `/rename <name>` assigns a local display name to the active chat/contact.
- `/emoji` opens the in-app emoji picker.
- `/send <path> [caption]` sends a file and auto-detects the media/document type.
- `/theme` shows the available built-in themes.
- `/theme1tokyonight` switches to the Tokyo Night theme.
- `/theme2catppuccin` switches to the Catppuccin theme.
- `/theme3monokai` switches to the Monokai theme.
- `/theme4charcoal` switches to the Charcoal theme.
- `/theme5aurora` switches to the Aurora theme.
- `/mouseon` enables mouse interactions in the TUI.
- `/mouseoff` disables mouse interactions in the TUI.
- `/logout` logs out the current WhatsApp session.
- `/restart` restarts the TUI process.
- `/exit` exits the TUI.

## Persistence Model

The app keeps local state so it can reopen quickly without rebuilding everything from scratch.

That local state currently covers:

- WhatsApp auth/session state
- cached chats
- cached messages
- contacts and resolved names
- whitelist permissions
- custom local names

This is one of the main reasons the app feels fast after the initial bootstrap.

## Call Support

Current status:

- **Detected:** yes
- **Notified in TUI:** yes
- **Answer / reject / media handling:** no

So the app can tell you that a call is happening while it is connected, but it is not a full calling client.

## Limitations

- Full voice/video call handling is not implemented.
- Live call detection is not the same as historical call sync.
- Emoji rendering still depends on terminal/font support even though emoji insertion is now internal.
- Historical backfill is limited by what WhatsApp Web / `whatsmeow` exposes.
- The app is still evolving, and there are likely rough edges around state sync and Windows terminal behavior.

## Tech Stack

- Go
- `go.mau.fi/whatsmeow`
- `github.com/charmbracelet/bubbletea`
- `github.com/charmbracelet/lipgloss`
- `github.com/gorilla/websocket`
- `modernc.org/sqlite`
