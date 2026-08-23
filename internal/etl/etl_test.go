// Package etl holds the sync-ETL building blocks (epic #88): a concurrency-safe
// registry of per-format handlers and a hardened file walker used by import
// paths. #96: ETL-handler registry + safe walker.
package etl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHandler is a minimal Handler for registry tests.
type fakeHandler struct{ name string }

func (f fakeHandler) Name() string                             { return f.name }
func (f fakeHandler) Handle(_ context.Context, _ string) error { return nil }

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := NewRegistry()
	h := fakeHandler{name: "mail"}
	if err := r.Register(h); err != nil {
		t.Fatalf("Register(mail): %v", err)
	}
	got, ok := r.Lookup("mail")
	if !ok {
		t.Fatal("Lookup(mail): not found")
	}
	if got.Name() != "mail" {
		t.Fatalf("Lookup(mail).Name() = %q, want mail", got.Name())
	}
	if _, ok := r.Lookup("git"); ok {
		t.Fatal("Lookup(git): unexpected hit for unregistered key")
	}
	if n := r.Len(); n != 1 {
		t.Fatalf("Len() = %d, want 1", n)
	}
}

func TestRegistryRejectsDuplicateKey(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeHandler{name: "mail"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := r.Register(fakeHandler{name: "mail"}); err == nil {
		t.Fatal("second Register with same key: want error, got nil")
	}
}

func TestRegistryNamesSorted(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"facts", "mail", "git", "markdown"} {
		if err := r.Register(fakeHandler{name: n}); err != nil {
			t.Fatalf("Register(%s): %v", n, err)
		}
	}
	names := r.Names()
	want := []string{"facts", "git", "mail", "markdown"}
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Names() = %v, want %v (sorted)", names, want)
		}
	}
}

func TestRegistryConcurrentSafe(t *testing.T) {
	r := NewRegistry()
	done := make(chan struct{})
	// Concurrent registrations.
	for i := 0; i < 64; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			_ = r.Register(fakeHandler{name: string(rune('a' + i))})
		}(i)
	}
	for i := 0; i < 64; i++ {
		<-done
	}
	// Concurrent lookups while nothing else mutates.
	for i := 0; i < 64; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			_, _ = r.Lookup(string(rune('a' + i)))
			_ = r.Names()
		}(i)
	}
	for i := 0; i < 64; i++ {
		<-done
	}
	if r.Len() != 64 {
		t.Fatalf("Len() = %d, want 64", r.Len())
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rels(files []File) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Rel)
	}
	return out
}

func TestWalkFilesDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "z.txt"), "z")
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "sub", "m.txt"), "m")
	files, err := WalkFiles(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	want := []string{"a.txt", "sub/m.txt", "z.txt"}
	got := rels(files)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v (deterministic sorted)", got, want)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3", len(files))
	}
}

func TestWalkTraversalRejected(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "sub")
	writeFile(t, filepath.Join(root, "in.txt"), "in")
	// A sibling secret directory OUTSIDE the walk root.
	writeFile(t, filepath.Join(parent, "secret.txt"), "secret")
	if err := os.Symlink("../secret.txt", filepath.Join(root, "leak")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	files, err := WalkFiles(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Rel, "leak") || strings.Contains(f.Rel, "..") {
			t.Fatalf("path traversal leaked into results: %v", rels(files))
		}
	}
	if len(files) != 1 {
		t.Fatalf("files = %v, want only in.txt (no traversal)", rels(files))
	}
}

func TestWalkSymlinkOutsideRejected(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(root, "keep.txt"), "keep")
	writeFile(t, filepath.Join(outside, "pwn.txt"), "pwn")
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	files, err := WalkFiles(root, WalkOptions{})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Rel, "pwn") || strings.Contains(f.Rel, "out") {
			t.Fatalf("symlink escape leaked into results: %v", rels(files))
		}
	}
	if len(files) != 1 {
		t.Fatalf("files = %v, want only keep.txt (no escape)", rels(files))
	}
}

func TestWalkDepthLimitHonored(t *testing.T) {
	root := t.TempDir()
	// Depth 1, 2, 3 relative to root.
	writeFile(t, filepath.Join(root, "l1.txt"), "1")
	writeFile(t, filepath.Join(root, "a", "l2.txt"), "2")
	writeFile(t, filepath.Join(root, "a", "b", "l3.txt"), "3")
	writeFile(t, filepath.Join(root, "a", "b", "c", "l4.txt"), "4")

	files, err := WalkFiles(root, WalkOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	got := rels(files)
	for _, rel := range got {
		if segDepth(rel) > 2 {
			t.Fatalf("depth limit violated: %q (depth %d) with MaxDepth=2", rel, segDepth(rel))
		}
	}
	if !containsStr(got, "a/l2.txt") {
		t.Fatalf("depth-2 file missing: %v", got)
	}
	if containsStr(got, "a/b/l3.txt") {
		t.Fatalf("depth-3 file should be skipped: %v", got)
	}
}

func TestWalkLargeFileSkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "small.txt"), "tiny")
	big := filepath.Join(root, "big.txt")
	if err := os.WriteFile(big, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := WalkFiles(root, WalkOptions{MaxBytes: 1024})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	got := rels(files)
	if len(got) != 1 || got[0] != "small.txt" {
		t.Fatalf("files = %v, want only small.txt (big.txt > MaxBytes skipped)", got)
	}
}

func TestWalkBinarySkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ok.txt"), "plain text, no null bytes\n")
	bin := filepath.Join(root, "bin.dat")
	if err := os.WriteFile(bin, append([]byte("P"), 0x00, 0x00, 0x00), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := WalkFiles(root, WalkOptions{SkipBinary: true})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	got := rels(files)
	if len(got) != 1 || got[0] != "ok.txt" {
		t.Fatalf("files = %v, want only ok.txt (bin.dat has null bytes)", got)
	}
}

func TestWalkExtsFilter(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# hi")
	writeFile(t, filepath.Join(root, "b.go"), "package x")
	files, err := WalkFiles(root, WalkOptions{Exts: []string{".md"}})
	if err != nil {
		t.Fatalf("WalkFiles: %v", err)
	}
	got := rels(files)
	if len(got) != 1 || got[0] != "a.md" {
		t.Fatalf("files = %v, want only a.md (ext filter)", got)
	}
}

func TestWalkMissingRootError(t *testing.T) {
	if _, err := WalkFiles(filepath.Join(t.TempDir(), "nope"), WalkOptions{}); err == nil {
		t.Fatal("WalkFiles on missing root: want error, got nil")
	}
}

func segDepth(rel string) int {
	return strings.Count(rel, string(os.PathSeparator)) + 1
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
