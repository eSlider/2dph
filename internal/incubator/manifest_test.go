package incubator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	m := Manifest{Entries: []Entry{
		{Key: "a@example.com", Path: "0000001/0000001.eml", Mailbox: "INBOX"},
		{Key: "b@example.com", Path: "INBOX_sbd/x_sbd/0000002/0000002.eml", Mailbox: "INBOX/x"},
	}}
	if err := m.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Entries) != 2 || got.Entries[0].Key != "a@example.com" || got.Entries[1].Mailbox != "INBOX/x" {
		t.Fatalf("roundtrip mismatch: %+v", got.Entries)
	}
}

func TestLoadManifestMissingFile(t *testing.T) {
	m, err := LoadManifest(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load(missing): %v", err)
	}
	if len(m.Entries) != 0 {
		t.Fatalf("expected empty manifest, got %d entries", len(m.Entries))
	}
}

func TestLoadManifestCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(p); err == nil {
		t.Fatal("Load(corrupt): expected error, got nil")
	}
}

func TestManifestIndex(t *testing.T) {
	m := Manifest{Entries: []Entry{{Key: "k@example.com", Path: "p", Mailbox: "INBOX"}}}
	if !m.Has("k@example.com") {
		t.Error("Has: want true for present key")
	}
	if m.Has("missing@example.com") {
		t.Error("Has: want false for absent key")
	}
}
