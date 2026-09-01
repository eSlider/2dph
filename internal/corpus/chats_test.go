package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/contract"
)

// TestChatsAdapterStreamsMessagesMd — messages.md с frontmatter id → leaf
// source=chats, external_id=id, kind=chat.
func TestChatsAdapterStreamsMessagesMd(t *testing.T) {
	dir := t.TempDir()
	md := "---\nid: 42\nplatform: telegram\nchat_id: 7\nchat_name: \"Alice\"\ntype: telegram\n---\n\n# Чат с Alice\n\n**2026-08-01** — Alice: hello\n"
	if err := os.MkdirAll(filepath.Join(dir, "telegram", "alice"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "telegram", "alice", "messages.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	leafs := collect(t, Chats{Dir: dir})
	if len(leafs) == 0 {
		t.Fatal("chats fixture must yield leafs")
	}
	for _, lf := range leafs {
		if lf.Source != "chats" {
			t.Errorf("source = %q, want chats", lf.Source)
		}
		if lf.ExternalID != "42" {
			t.Errorf("external_id = %q, want frontmatter id 42", lf.ExternalID)
		}
		if lf.Kind != "telegram" {
			t.Errorf("kind = %q, want frontmatter type telegram", lf.Kind)
		}
		if !strings.Contains(lf.Text, "Alice") {
			t.Errorf("text = %q, want chat content", lf.Text)
		}
		if !strings.HasSuffix(filepath.ToSlash(lf.Loc), "messages.md") {
			t.Errorf("loc = %q, want messages.md path", lf.Loc)
		}
		if err := lf.Validate(); err != nil {
			t.Errorf("leaf invalid: %v", err)
		}
	}
}

// TestChatsAdapterFallbackContentAddr — без frontmatter id external_id падает
// на content-address (как mail), leaf остаётся валидным.
func TestChatsAdapterFallbackContentAddr(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "whatsapp", "bob"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whatsapp", "bob", "messages.md"), []byte("# Чат с Bob\n\n**2026-08-02** — Bob: hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	leafs := collect(t, Chats{Dir: dir})
	if len(leafs) == 0 {
		t.Fatal("chats fixture must yield leafs")
	}
	for _, lf := range leafs {
		if lf.Kind != "chat" {
			t.Errorf("kind = %q, want default chat", lf.Kind)
		}
		if len(lf.ExternalID) != 16 {
			t.Errorf("external_id = %q, want 16-hex content address fallback", lf.ExternalID)
		}
		if err := lf.Validate(); err != nil {
			t.Errorf("leaf invalid: %v", err)
		}
	}
}

func TestChatsAdapterImplementsSource(t *testing.T) {
	var _ contract.Source = Chats{}
}
