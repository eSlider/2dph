package chat

// WhatsApp history sync (#71): convert gowa/whatsmeow app-state dumps
// (history-*.json in the tools_gowa-whatsapp volume) into the same
// messages.jsonl layout the telegram importer consumes.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"time"
)

type waMessage struct {
	Key struct {
		ID        string `json:"ID"`
		RemoteJID string `json:"RemoteJID"`
		FromMe    bool   `json:"FromMe"`
	} `json:"key"`
	Message            map[string]any `json:"message"`
	MessageTimestamp   any            `json:"messageTimestamp"`
	PushName           string         `json:"pushName"`
	OriginalSelfAuthor string         `json:"originalSelfAuthorUserJIDString"`
}

type waConversation struct {
	ID       string `json:"ID"`
	Messages []struct {
		Message waMessage `json:"message"`
	} `json:"messages"`
}

type waHistory struct {
	Conversations []waConversation `json:"conversations"`
	Pushnames     []struct {
		JID      string `json:"JID"`
		PushName string `json:"PushName"`
	} `json:"pushnames"`
}

// waText digs the human-readable text out of a whatsmeow message body.
func waText(inner map[string]any) string {
	for _, key := range []string{
		"conversation", "extendedTextMessage", "imageMessage",
		"videoMessage", "documentMessage", "audioMessage", "stickerMessage",
		"contactMessage", "locationMessage", "liveLocationMessage",
	} {
		v, ok := inner[key]
		if !ok {
			continue
		}
		switch key {
		case "conversation":
			s, _ := v.(string)
			return s
		default:
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			for _, tk := range []string{"text", "caption"} {
				if s, ok := m[tk].(string); ok && s != "" {
					return s
				}
			}
			if key == "imageMessage" || key == "videoMessage" || key == "stickerMessage" {
				return "[media]"
			}
			if key == "audioMessage" {
				return "[audio]"
			}
			if key == "documentMessage" {
				return "[document]"
			}
		}
	}
	return ""
}

func RunSyncWhatsApp(args []string) int {
	var from, out string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from":
			if i+1 < len(args) {
				i++
				from = args[i]
			}
		case "--out":
			if i+1 < len(args) {
				i++
				out = args[i]
			}
		}
	}
	if from == "" {
		fmt.Fprintln(os.Stderr, "usage: chats sync whatsapp --from DIR [ --out DIR ]")
		return 2
	}
	root := Dir()
	if out == "" {
		out = filepath.Join(root, "whatsapp")
	}
	dumps, err := filepath.Glob(filepath.Join(from, "history-*.json"))
	if err != nil || len(dumps) == 0 {
		fmt.Fprintf(os.Stderr, "chats-sync-whatsapp: no history-*.json under %s\n", from)
		return 2
	}
	sort.Strings(dumps)

	pushnames := map[string]string{}
	chats := map[string]map[string]Message{} // jid -> msgID -> entry (dedupe across dumps)

	for _, f := range dumps {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var h waHistory
		if json.Unmarshal(data, &h) != nil {
			continue
		}
		for _, pn := range h.Pushnames {
			if pn.PushName != "" {
				pushnames[pn.JID] = pn.PushName
			}
		}
		for _, conv := range h.Conversations {
			jid := conv.ID
			if chats[jid] == nil {
				chats[jid] = map[string]Message{}
			}
			for _, wm := range conv.Messages {
				m := wm.Message
				id := m.Key.ID
				if id == "" {
					continue
				}
				var inner map[string]any
				if m.Message != nil {
					inner = m.Message
				}
				text := waText(inner)
				if strings.TrimSpace(text) == "" {
					continue // protocol/reaction/empty frames carry no content
				}
				from := m.PushName
				if from == "" && !m.Key.FromMe {
					from = pushnames[jid]
				}
				if from == "" {
					from = jid
				}
				if m.Key.FromMe {
					from = "Me"
				}
				ts := ""
				switch t := m.MessageTimestamp.(type) {
				case float64:
					ts = timeUnix(int64(t))
				case string:
					if n, err := strconv.ParseInt(t, 10, 64); err == nil {
						ts = timeUnix(n)
					}
				case int64:
					ts = timeUnix(t)
				}
				chats[jid][id] = Message{
					ID: id, Timestamp: ts, From: from,
					Text: text, Platform: "whatsapp",
				}
			}
		}
	}

	written := 0
	for jid, msgs := range chats {
		if len(msgs) == 0 {
			continue
		}
		dir := sanitizeDir(pushnameOr(jid, pushnames))
		dstDir := filepath.Join(out, dir)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			continue
		}
		f, err := os.Create(filepath.Join(dstDir, "messages.jsonl"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "chats-sync-whatsapp: %v\n", err)
			return 1
		}
		enc := json.NewEncoder(f)
		names := make([]string, 0, len(msgs))
		for id := range msgs {
			names = append(names, id)
		}
		sort.Strings(names)
		for _, id := range names {
			_ = enc.Encode(msgs[id])
		}
		f.Close()
		written += len(msgs)
	}
	fmt.Printf("chats sync whatsapp: %d messages → %d chats under %s\n", written, len(chats), out)
	return 0
}

func pushnameOr(jid string, pushnames map[string]string) string {
	if n := pushnames[jid]; n != "" {
		return n + "_" + jidShort(jid)
	}
	return jid
}

func jidShort(jid string) string {
	if i := strings.Index(jid, "@"); i > 0 {
		return jid[maxInt(0, i-6):]
	}
	return jid
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func timeUnix(n int64) string {
	return time.Unix(n, 0).UTC().Format("2006-01-02T15:04:05Z")
}
