package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gorilla/websocket"
)

func ensureBackend(c *http.Client, base, dir string) tea.Cmd {
	return func() tea.Msg {
		if health(c, base) == nil {
			return initMsg{}
		}
		goBin := "go"
		if runtime.GOOS == "windows" {
			goBin = "go.exe"
		}
		cmd := exec.Command(goBin, "run", ".")
		cmd.Dir = dir
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Start(); err != nil {
			return initMsg{err: err}
		}
		deadline := time.Now().Add(35 * time.Second)
		for time.Now().Before(deadline) {
			if health(c, base) == nil {
				return initMsg{started: true, cmd: cmd}
			}
			time.Sleep(400 * time.Millisecond)
		}
		return initMsg{err: fmt.Errorf("backend did not become ready")}
	}
}

func health(c *http.Client, base string) error {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/health", nil)
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("health status %s", res.Status)
	}
	return nil
}
func openWS(url string) tea.Cmd {
	return func() tea.Msg {
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			return wsOpenMsg{err: err}
		}
		ch := make(chan env)
		go func() {
			defer close(ch)
			for {
				_, b, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var e env
				if json.Unmarshal(b, &e) == nil {
					ch <- e
				}
			}
		}()
		return wsOpenMsg{conn: conn, ch: ch}
	}
}
func readWS(ch <-chan env) tea.Cmd {
	return func() tea.Msg { e, ok := <-ch; return wsEvtMsg{evt: e, ok: ok} }
}
func postEmpty(c *http.Client, url string, ok func([]byte) tea.Msg) tea.Cmd {
	return postJSON(c, url, map[string]string{}, ok)
}
func postJSON(c *http.Client, url string, body any, ok func([]byte) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		b, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(b))
		req.Header.Set("content-type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			return dataErr{err: err}
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode/100 != 2 {
			return dataErr{err: fmt.Errorf("%s %s", res.Status, strings.TrimSpace(string(raw)))}
		}
		if ok != nil {
			return ok(raw)
		}
		return dataErr{}
	}
}
func getChats(c *http.Client, base string) tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/chats", nil)
		res, err := c.Do(req)
		if err != nil {
			return chatsMsg{err: err}
		}
		defer res.Body.Close()
		var out struct {
			Chats []chat `json:"chats"`
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return chatsMsg{err: err}
		}
		return chatsMsg{chats: out.Chats}
	}
}
func getContacts(c *http.Client, base string) tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/contacts", nil)
		res, err := c.Do(req)
		if err != nil {
			return contactsMsg{err: err}
		}
		defer res.Body.Close()
		var out struct {
			Contacts []contact `json:"contacts"`
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return contactsMsg{err: err}
		}
		return contactsMsg{contacts: out.Contacts}
	}
}
func getMsgs(c *http.Client, base, chatID string, limit int) tea.Cmd {
	return func() tea.Msg {
		u := fmt.Sprintf("%s/messages?chatId=%s&limit=%d", base, chatID, limit)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		res, err := c.Do(req)
		if err != nil {
			return msgsMsg{chatID: chatID, err: err}
		}
		defer res.Body.Close()
		var out struct {
			Messages []wireMsg `json:"messages"`
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return msgsMsg{chatID: chatID, err: err}
		}
		return msgsMsg{chatID: chatID, msgs: out.Messages}
	}
}
func send(c *http.Client, base, chatID, text string, replyTo *wireMsg) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]any{"chatId": chatID, "text": text}
		if replyTo != nil {
			payload["replyToMsgId"] = replyTo.Key.ID
			payload["replyToText"] = renderMessageBody(replyTo.Message)
			participant := replyTo.Key.RemoteJID
			if replyTo.Key.Participant != "" {
				participant = replyTo.Key.Participant
			}
			if replyTo.Key.FromMe {
				participant = ""
			}
			payload["replyToParticipant"] = participant
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/messages/send", bytes.NewReader(b))
		req.Header.Set("content-type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			return sentMsg{chatID: chatID, err: err}
		}
		defer res.Body.Close()
		if res.StatusCode/100 != 2 {
			raw, _ := io.ReadAll(res.Body)
			return sentMsg{chatID: chatID, err: fmt.Errorf("%s %s", res.Status, strings.TrimSpace(string(raw)))}
		}
		var out struct {
			Message wireMsg `json:"message"`
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return sentMsg{chatID: chatID, err: err}
		}
		return sentMsg{chatID: chatID, msg: out.Message}
	}
}

func sendFile(c *http.Client, base, chatID, kind, path, caption string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]any{
			"chatId":  chatID,
			"kind":    kind,
			"path":    path,
			"caption": caption,
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/messages/send-file", bytes.NewReader(b))
		req.Header.Set("content-type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			return sentMsg{chatID: chatID, err: err}
		}
		defer res.Body.Close()
		if res.StatusCode/100 != 2 {
			raw, _ := io.ReadAll(res.Body)
			return sentMsg{chatID: chatID, err: fmt.Errorf("%s %s", res.Status, strings.TrimSpace(string(raw)))}
		}
		var out struct {
			Message wireMsg `json:"message"`
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return sentMsg{chatID: chatID, err: err}
		}
		return sentMsg{chatID: chatID, msg: out.Message}
	}
}
func (x m) cleanup() {
	if x.demoMode {
		return
	}
	if x.ws != nil {
		_ = x.ws.Close()
	}
	if !x.startedBackend || x.backend == nil || x.backend.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/PID", strconv.Itoa(x.backend.Process.Pid), "/T", "/F").Run()
		return
	}
	_ = x.backend.Process.Kill()
}

func getWhitelist(c *http.Client, base string) tea.Cmd {
	return func() tea.Msg {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/whitelist", nil)
		res, err := c.Do(req)
		if err != nil {
			return whitelistLoadMsg{err: err}
		}
		defer res.Body.Close()
		var out struct {
			Contacts []struct {
				Phone   string `json:"phone"`
				Name    string `json:"name"`
				Allowed int    `json:"allowed"`
			} `json:"contacts"`
		}
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return whitelistLoadMsg{err: err}
		}
		wl := map[string]string{}
		names := map[string]string{}
		for _, e := range out.Contacts {
			if e.Allowed == 1 {
				wl[e.Phone] = e.Name
			}
			if e.Name != "" {
				names[e.Phone] = e.Name
			}
		}
		return whitelistLoadMsg{whitelist: wl, names: names}
	}
}

func downloadMedia(c *http.Client, base, chatID, msgID string) tea.Cmd {
	return func() tea.Msg {
		url := base + "/media/download?chatId=" + chatID + "&msgId=" + msgID
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		res, err := c.Do(req)
		if err != nil {
			return mediaDownloadMsg{err: err}
		}
		defer res.Body.Close()
		var out struct {
			Path  string `json:"path"`
			Error string `json:"error"`
		}
		json.NewDecoder(res.Body).Decode(&out)
		if out.Error != "" {
			return mediaDownloadMsg{err: fmt.Errorf("%s", out.Error)}
		}
		return mediaDownloadMsg{path: out.Path}
	}
}

func openFile(path string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("cmd", "/c", "start", "", path)
		case "darwin":
			cmd = exec.Command("open", path)
		default:
			cmd = exec.Command("xdg-open", path)
		}
		_ = cmd.Start()
		return nil
	}
}

func setName(c *http.Client, base, phone, name string) tea.Cmd {
	return func() tea.Msg {
		b, _ := json.Marshal(map[string]any{"phone": phone, "name": name})
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/names/set", bytes.NewReader(b))
		req.Header.Set("content-type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			return whitelistSetMsg{err: err}
		}
		res.Body.Close()
		return whitelistSetMsg{}
	}
}

func setWhitelistEntry(c *http.Client, base, phone, name string, allowed int) tea.Cmd {
	return func() tea.Msg {
		b, _ := json.Marshal(map[string]any{"phone": phone, "name": name, "allowed": allowed})
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/whitelist/set", bytes.NewReader(b))
		req.Header.Set("content-type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			return whitelistSetMsg{err: err}
		}
		defer res.Body.Close()
		if res.StatusCode/100 != 2 {
			raw, _ := io.ReadAll(res.Body)
			return whitelistSetMsg{err: fmt.Errorf("%s %s", res.Status, strings.TrimSpace(string(raw)))}
		}
		return whitelistSetMsg{}
	}
}

func syncContacts(c *http.Client, base string) tea.Cmd {
	return postEmpty(c, base+"/sync/contacts", func(raw []byte) tea.Msg {
		var out struct {
			Updated    int `json:"updated"`
			Enriched   int `json:"enriched"`
			Queried    int `json:"queried"`
			Unresolved int `json:"unresolved"`
			Total      int `json:"total"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return topBarSetMsg{msg: "Contacts sync complete"}
		}
		return topBarSetMsg{msg: fmt.Sprintf("Sync: local %d, server %d/%d, unresolved %d", out.Updated, out.Enriched, out.Queried, out.Unresolved)}
	})
}

func syncGroups(c *http.Client, base string) tea.Cmd {
	return postEmpty(c, base+"/sync/groups", func(raw []byte) tea.Msg {
		var out struct {
			Updated int `json:"updated"`
			Total   int `json:"total"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return syncGroupsDoneMsg{msg: "Groups sync complete"}
		}
		return syncGroupsDoneMsg{msg: fmt.Sprintf("Groups synced: %d/%d updated", out.Updated, out.Total)}
	})
}
