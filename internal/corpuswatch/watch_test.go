package corpuswatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStampChangesWhenFileTouched(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	if err := os.WriteFile(a, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s1 := Stamp([]string{dir})
	if s1 == "" {
		t.Fatal("stamp empty for a dir with a file")
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(a, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s2 := Stamp([]string{dir}); s2 == s1 {
		t.Fatal("stamp did not change after the file was modified")
	}
}

func TestStampEmptyForMissingDir(t *testing.T) {
	if s := Stamp([]string{filepath.Join(t.TempDir(), "nope")}); s != "" {
		t.Fatalf("stamp = %q, want empty for missing dir", s)
	}
}

func TestFromEnvDefaults(t *testing.T) {
	t.Setenv("KB_WATCH_INTERVAL", "")
	t.Setenv("KB_WATCH_DIRS", "")
	opts := fromEnv(nil)
	if len(opts.Dirs) == 0 || opts.Dirs[0] != "/corpus" {
		t.Fatalf("default dirs = %v, want [/corpus]", opts.Dirs)
	}
	if opts.Interval != 30*time.Second {
		t.Fatalf("default interval = %s, want 30s", opts.Interval)
	}
	if !strings.Contains(opts.IndexCmd, "brain/index.go") {
		t.Fatalf("default index cmd = %q, want brain/index.go", opts.IndexCmd)
	}
	if !strings.Contains(opts.IndexCmd, "--with-mail") {
		t.Fatalf("default index cmd must include --with-mail, got %q", opts.IndexCmd)
	}
}
