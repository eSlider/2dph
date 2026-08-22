// Unit tests for the WhatsApp history sync (internal/chat/sync_whatsapp.go).
// Offline, synthetic fixtures, no network.
package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWAText(t *testing.T) {
	cases := []struct {
		name  string
		inner map[string]any
		want  string
	}{
		{"conversation", map[string]any{"conversation": "hello world"}, "hello world"},
		{"extendedText", map[string]any{"extendedTextMessage": map[string]any{"text": "long text"}}, "long text"},
		{"imageCaption", map[string]any{"imageMessage": map[string]any{"caption": "pic caption"}}, "pic caption"},
		{"imageNoText", map[string]any{"imageMessage": map[string]any{}}, "[media]"},
		{"video", map[string]any{"videoMessage": map[string]any{}}, "[media]"},
		{"audio", map[string]any{"audioMessage": map[string]any{}}, "[audio]"},
		{"document", map[string]any{"documentMessage": map[string]any{}}, "[document]"},
		{"sticker", map[string]any{"stickerMessage": map[string]any{}}, "[media]"},
		{"empty", map[string]any{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := waText(c.inner); got != c.want {
				t.Fatalf("waText() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestJIDShort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1203630123456789@g.us", "456789@g.us"},
		{"no-at-sign", "no-at-sign"},
		{"short", "short"},
		{"@", "@"},
	}
	for _, c := range cases {
		if got := jidShort(c.in); got != c.want {
			t.Errorf("jidShort(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMaxInt(t *testing.T) {
	if maxInt(5, 3) != 5 {
		t.Error("maxInt(5,3) != 5")
	}
	if maxInt(2, 9) != 9 {
		t.Error("maxInt(2,9) != 9")
	}
	if maxInt(4, 4) != 4 {
		t.Error("maxInt(4,4) != 4")
	}
}

func TestTimeUnix(t *testing.T) {
	got := timeUnix(1700000000)
	want := "2023-11-14T22:13:20Z"
	if got != want {
		t.Fatalf("timeUnix(1700000000) = %q, want %q", got, want)
	}
}

func TestPlatformOf(t *testing.T) {
	root := "/data/chats"
	cases := []struct{ path, want string }{
		{"/data/chats/telegram/room/messages.jsonl", "telegram"},
		{"/data/chats/whatsapp/room/messages.jsonl", "whatsapp"},
		{"/data/chats/whatsupp/room/messages.jsonl", ""},
		{"/elsewhere/telegram/room/messages.jsonl", ""},
	}
	for _, c := range cases {
		if got := platformOf(c.path, root); got != c.want {
			t.Errorf("platformOf(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestRunImportRejectsUnknownPlatform ensures a typo like "whatsupp" is
// rejected instead of silently ingested (review blocker 2).
func TestRunImportRejectsUnknownPlatform(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })
	t.Setenv("KB_ROOT", dir)

	if code := RunImport([]string{"whatsupp"}); code == 0 {
		t.Fatal("expected non-zero exit for unknown platform")
	}
}

// whatsappFixture builds a synthetic history-*.json dump and returns the
// directory holding it.
func whatsappFixture(t *testing.T, convID string, msgs []map[string]any) string {
	t.Helper()
	src := t.TempDir()
	wrapped := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		wrapped = append(wrapped, map[string]any{"message": m})
	}
	hist := map[string]any{
		"conversations": []map[string]any{
			{"ID": convID, "messages": wrapped},
		},
		"pushnames": []map[string]any{
			{"JID": convID, "PushName": "Team Room"},
		},
	}
	data, err := json.Marshal(hist)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "history-1.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	return src
}

func waMsg(id string, fromMe bool, ts int64, pushName, text string) map[string]any {
	inner := map[string]any{"conversation": text}
	key := map[string]any{"ID": id, "RemoteJID": "room@g.us", "FromMe": fromMe}
	return map[string]any{"key": key, "message": inner, "messageTimestamp": ts, "pushName": pushName}
}

// TestRunSyncWhatsApp covers the end-to-end jsonl write path with a synthetic
// dump (review blocker 1).
func TestRunSyncWhatsApp(t *testing.T) {
	src := whatsappFixture(t, "1203630123456789@g.us", []map[string]any{
		waMsg("Z2", false, 1700000000, "Alice", "first"),
		waMsg("A1", false, 1700000010, "Bob", "second"),
	})
	out := t.TempDir()

	if code := RunSyncWhatsApp([]string{"--from", src, "--out", out}); code != 0 {
		t.Fatalf("RunSyncWhatsApp exit %d", code)
	}

	var jsonlPath string
	filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, "messages.jsonl") {
			jsonlPath = p
		}
		return nil
	})
	if jsonlPath == "" {
		t.Fatal("no messages.jsonl written")
	}

	data, err := os.ReadFile(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(lines))
	}
	// Chronological order regardless of opaque message-ID (review blocker 3).
	if !strings.Contains(lines[0], "first") {
		t.Errorf("line 0 should be the earliest message, got %q", lines[0])
	}
	if !strings.Contains(lines[1], "second") {
		t.Errorf("line 1 should be the latest message, got %q", lines[1])
	}
	for _, l := range lines {
		if !strings.Contains(l, `"platform":"whatsapp"`) {
			t.Errorf("message missing platform field: %q", l)
		}
	}
}

// TestRunSyncWhatsAppChronological explicitly forces message IDs that would
// sort in reverse chronological order and asserts output is time-ordered.
func TestRunSyncWhatsAppChronological(t *testing.T) {
	src := whatsappFixture(t, "g1", []map[string]any{
		waMsg("AAA", false, 1700000200, "Bob", "later id sorts first"),
		waMsg("ZZZ", false, 1700000100, "Alice", "earlier"),
	})
	out := t.TempDir()
	if code := RunSyncWhatsApp([]string{"--from", src, "--out", out}); code != 0 {
		t.Fatalf("RunSyncWhatsApp exit %d", code)
	}
	data, _ := os.ReadFile(filepath.Join(out, "Team_Room_g1", "messages.jsonl"))
	content := string(data)
	earlier := strings.Index(content, "earlier")
	later := strings.Index(content, "later id sorts first")
	if earlier == -1 || later == -1 {
		t.Fatalf("missing messages in output:\n%s", content)
	}
	if earlier > later {
		t.Fatalf("messages not chronologically ordered:\n%s", content)
	}
}

// TestRunSyncWhatsAppEmptyFrom asserts a missing --from is rejected.
func TestRunSyncWhatsAppEmptyFrom(t *testing.T) {
	if code := RunSyncWhatsApp([]string{}); code == 0 {
		t.Fatal("expected non-zero exit when --from is missing")
	}
}
