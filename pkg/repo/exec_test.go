package repo

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

func TestExecFileMissingIs127(t *testing.T) {
	t.Setenv("KB_ROOT", t.TempDir())
	if code := ExecFile("no/such-tool", nil); code != 127 {
		t.Fatalf("exit = %d, want 127", code)
	}
}

func TestExecFileRuns(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "echo.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KB_ROOT", root)
	if code := ExecFile("echo.sh", nil); code != 3 {
		t.Fatalf("exit = %d, want 3", code)
	}
}
