// Tests for the index-loop --corpus flag + BRAIN_CORPUS parsing (#317).
// Offline, no db.
package indexsync

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestStringListRepeats: the flag value must accumulate repeated --corpus.
func TestStringListRepeats(t *testing.T) {
	var s StringList
	if err := s.Set("/skills"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("/extra"); err != nil {
		t.Fatal(err)
	}
	if got := []string(s); !reflect.DeepEqual(got, []string{"/skills", "/extra"}) {
		t.Fatalf("StringList = %v, want [/skills /extra]", got)
	}
}

// TestSplitCorpusEnv: comma-separated, whitespace trimmed, empties dropped,
// order preserved.
func TestSplitCorpusEnv(t *testing.T) {
	got := SplitCorpusEnv(" /skills , /extra/corpus ,,   ")
	want := []string{"/skills", "/extra/corpus"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitCorpusEnv = %v, want %v", got, want)
	}
	if got := SplitCorpusEnv(""); got != nil {
		t.Fatalf("SplitCorpusEnv(empty) = %v, want nil", got)
	}
}

// TestResolveCorpus: repeatable flags win over the env; with no flags the env
// is parsed; blank flags fall through to the env.
func TestResolveCorpus(t *testing.T) {
	cases := []struct {
		name  string
		flags []string
		env   string
		want  []string
	}{
		{"flags", []string{"/a", "/b"}, "/ignored", []string{"/a", "/b"}},
		{"env", nil, " /a , /b ", []string{"/a", "/b"}},
		{"empty", nil, "", nil},
		{"blank flags fall back", []string{" ", ""}, "/env", []string{"/env"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveCorpus(tc.flags, tc.env); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ResolveCorpus(%v, %q) = %v, want %v", tc.flags, tc.env, got, tc.want)
			}
		})
	}
}

// writeCorpusFile writes a file (creating parents) and stamps its mtime.
func writeCorpusFile(t *testing.T, path string, mtime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// TestCorpusNewest: the max file mtime under the given dirs; absent dirs count
// as zero (not an error); directories and symlinks are skipped (#319).
func TestCorpusNewest(t *testing.T) {
	if got := corpusNewest(nil); got != 0 {
		t.Fatalf("corpusNewest(nil) = %d, want 0", got)
	}
	dir := t.TempDir()
	if got := corpusNewest([]string{dir}); got != 0 {
		t.Fatalf("corpusNewest(empty dir) = %d, want 0", got)
	}
	if got := corpusNewest([]string{missingDir(t)}); got != 0 {
		t.Fatalf("corpusNewest(absent dir) = %d, want 0", got)
	}

	old := time.Now().Add(-48 * time.Hour)
	fresh := time.Now().Add(-1 * time.Hour)
	writeCorpusFile(t, filepath.Join(dir, "old.md"), old)
	writeCorpusFile(t, filepath.Join(dir, "sub", "fresh.md"), fresh)

	// A symlink with a newer target must be skipped, not followed.
	target := filepath.Join(t.TempDir(), "newer.md")
	writeCorpusFile(t, target, time.Now())
	if err := os.Symlink(target, filepath.Join(dir, "link.md")); err != nil {
		t.Fatal(err)
	}

	if got, want := corpusNewest([]string{dir}), fresh.UnixNano(); got != want {
		t.Fatalf("corpusNewest = %d, want %d", got, want)
	}
}
