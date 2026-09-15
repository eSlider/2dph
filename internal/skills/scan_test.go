package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMissingBinRefsReportsMissing(t *testing.T) {
	root := t.TempDir()
	write(t, root, "bin/brain/search.go", "package main\n")
	write(t, root, "skills/brain/SKILL.md", "Use `bin/brain/search.go` first, not `bin/agents/cost`.\n")

	got, err := MissingBinRefs(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"skills/brain/SKILL.md: bin/agents/cost"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestMissingBinRefsOK(t *testing.T) {
	root := t.TempDir()
	write(t, root, "bin/brain/search.go", "package main\n")
	write(t, root, "bin/cgo/zig", "#!/bin/sh\n")
	write(t, root, "skills/brain/SKILL.md", "`bin/brain/search.go` and `./bin/cgo/zig env`.\n")

	got, err := MissingBinRefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no findings, got %v", got)
	}
}

func TestMissingBinRefsIgnoresNonBinAndPlaceholders(t *testing.T) {
	root := t.TempDir()
	write(t, root, "skills/x/SKILL.md", "Run `scripts/db/psql-yq` and `bin/{subject}/{verb}-{object}.go`.\n")

	got, err := MissingBinRefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no findings, got %v", got)
	}
}

func TestMissingBinRefsTrimsTrailingPunctuation(t *testing.T) {
	root := t.TempDir()
	write(t, root, "bin/brain/search.go", "package main\n")
	write(t, root, "skills/x/SKILL.md", "See (bin/brain/search.go).\n")

	got, err := MissingBinRefs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no findings, got %v", got)
	}
}

func TestMissingBinRefsDedup(t *testing.T) {
	root := t.TempDir()
	write(t, root, "skills/x/SKILL.md", "bin/gone.go then bin/gone.go again.\n")

	got, err := MissingBinRefs(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"skills/x/SKILL.md: bin/gone.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
