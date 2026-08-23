package canon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A synthetic mail input (Alice -> Bob, CC Carol, with a reply link and an
// attachment) must round-trip through disk storage: Write -> Read -> canon.
func TestStoreRoundTripMail(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewStore(root)
	m := buildMail()

	written, err := s.Write(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if !written {
		t.Fatal("first write reported not-written")
	}

	got, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("Read returned %d messages, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0], m) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got[0], m)
	}
}

// A chat message stored alongside a mail message lives under its own platform
// directory; both are read back in one pass.
func TestStoreRoundTripChat(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewStore(root)
	mail := buildMail()
	chat := buildChat()

	if _, err := s.Write(ctx, mail); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(ctx, chat); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("Read returned %d messages, want 2", len(got))
	}
	foundMail, foundChat := false, false
	for _, m := range got {
		switch m.Platform {
		case "mail":
			foundMail = true
		case "telegram":
			foundChat = true
		}
	}
	if !foundMail || !foundChat {
		t.Fatalf("expected both platforms, got %+v", got)
	}
}

// Re-writing the same message with unchanged body is idempotent: it reports
// not-written and does not grow the manifest.
func TestStoreIdempotentUpsert(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewStore(root)
	m := buildMail()

	if _, err := s.Write(ctx, m); err != nil {
		t.Fatal(err)
	}
	second, err := s.Write(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Fatal("second identical write reported written; expected idempotent skip")
	}

	mf, err := s.LoadManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mf.Messages) != 1 {
		t.Fatalf("manifest has %d entries, want 1", len(mf.Messages))
	}
}

// The manifest records sha256(canonical JSON body) per message ID — the dedup
// key that lets a sync wave skip unchanged messages.
func TestManifestHashMatchesCanonicalBody(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewStore(root)
	m := buildMail()

	if _, err := s.Write(ctx, m); err != nil {
		t.Fatal(err)
	}
	mf, err := s.LoadManifest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := mf.Hash(m.ID)
	if !ok {
		t.Fatalf("manifest missing id %q", m.ID)
	}

	body, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	want := hex.EncodeToString(sum[:])
	if got != want {
		t.Fatalf("manifest hash = %s, want %s", got, want)
	}
}

// Files land at root/<platform>/<thread>/<id>.json so the corpus is greppable
// and the platform/thread split is stable.
func TestStoreLayout(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s := NewStore(root)
	if _, err := s.Write(ctx, buildMail()); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "mail", "T42", "m1.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected message file at %s: %v", p, err)
	}
	if _, err := os.Stat(filepath.Join(root, "manifest.json")); err != nil {
		t.Fatalf("expected manifest at root: %v", err)
	}
}
