package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootHonorsKBROOT(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KB_ROOT", dir)
	if got := Root(); got != dir {
		t.Fatalf("Root() = %q, want %q", got, dir)
	}
}

func TestRootWalksUpForGitDir(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KB_ROOT", "")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(inner); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if got := Root(); got != root {
		t.Fatalf("Root() = %q, want %q", got, root)
	}
}

func TestRootWalksUpForVarDir(t *testing.T) {
	root := t.TempDir()
	inner := filepath.Join(root, "x", "y")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "var"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KB_ROOT", "")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(inner); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	if got := Root(); got != root {
		t.Fatalf("Root() = %q, want %q", got, root)
	}
}
