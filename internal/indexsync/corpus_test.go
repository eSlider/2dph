// Tests for the index-loop --corpus flag + BRAIN_CORPUS parsing (#317).
// Offline, no db.
package indexsync

import (
	"reflect"
	"testing"
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
