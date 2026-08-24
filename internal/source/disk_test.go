package source

import (
	"os"
	"path/filepath"
	"testing"
)

// Disk emits one Blob per .eml under Root. Non-.eml files and the placeholder
// file are ignored.
func TestDiskSourceYieldsEML(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.eml"), "From: alice@example.com\nSubject: one\n\nhi\n")
	mustWrite(t, filepath.Join(root, "sub", "b.eml"), "From: bob@example.com\nSubject: two\n\nhey\n")
	mustWrite(t, filepath.Join(root, "note.txt"), "not mail")

	src := &Disk{Root: root}
	blobs, next, err := src.Fetch(ctx(), "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(blobs) != 2 {
		t.Fatalf("got %d blobs, want 2: %+v", len(blobs), blobs)
	}
	if next != "" {
		t.Fatalf("disk source must keep cursor stable, got %q", next)
	}
	for _, b := range blobs {
		if filepath.Ext(b.Path) != ".eml" {
			t.Fatalf("blob path %q is not .eml", b.Path)
		}
	}
}

// End-to-end: disk source over a Sync run is idempotent — a second run with
// unchanged files yields 0 new blobs (sha256 seen-set).
func TestDiskSourceSyncIdempotent(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.eml"), "mail one")
	mustWrite(t, filepath.Join(root, "b.eml"), "mail two")
	state := filepath.Join(t.TempDir(), "disk.json")

	src := &Disk{Root: root}
	var handled []string
	st1, err := Sync(ctx(), src, handleCollect(&handled), Options{StatePath: state})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if st1.New != 2 {
		t.Fatalf("first New = %d, want 2", st1.New)
	}
	if len(handled) != 2 {
		t.Fatalf("first run handled %d items, want 2", len(handled))
	}

	st2, err := Sync(ctx(), src, handleCollect(&handled), Options{StatePath: state})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if st2.New != 0 {
		t.Fatalf("second New = %d, want 0 (seen-set must dedup)", st2.New)
	}
	if len(handled) != 2 {
		t.Fatalf("second run re-handled %d items, want still 2 (seen-set dedup)", len(handled))
	}
}

// A changed .eml is a new blob (content-identity), so it is re-emitted once.
func TestDiskSourceContentChangedIsNewBlob(t *testing.T) {
	root := t.TempDir()
	f := filepath.Join(root, "a.eml")
	mustWrite(t, f, "version one")
	state := filepath.Join(t.TempDir(), "disk.json")

	src := &Disk{Root: root}
	var handled []string
	if st, err := Sync(ctx(), src, handleCollect(&handled), Options{StatePath: state}); err != nil || st.New != 1 {
		t.Fatalf("first sync: st=%+v err=%v", st, err)
	}

	mustWrite(t, f, "version two") // same path, new content
	if st, err := Sync(ctx(), src, handleCollect(&handled), Options{StatePath: state}); err != nil {
		t.Fatalf("second sync: %v", err)
	} else if st.New != 1 {
		t.Fatalf("changed content New = %d, want 1", st.New)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
