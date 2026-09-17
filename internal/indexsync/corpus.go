// Corpus flag/env plumbing for the index-loop CLI (#317): the skills tree is
// indexed by brain-index --corpus as part of the periodic cycle.
package indexsync

import "strings"

// StringList is a repeatable string flag value (flag.Value): every Set appends
// in order, so `--corpus a --corpus b` yields [a b].
type StringList []string

func (s *StringList) String() string { return strings.Join(*s, ",") }

func (s *StringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// SplitCorpusEnv parses BRAIN_CORPUS: comma-separated dirs, whitespace
// trimmed, empties dropped, order preserved.
func SplitCorpusEnv(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ResolveCorpus returns the effective --corpus list: repeatable flag values
// (trimmed, blanks dropped) win; with no flag value the BRAIN_CORPUS env is
// parsed. An empty result means no --corpus flag is emitted.
func ResolveCorpus(flags []string, env string) []string {
	var out []string
	for _, f := range flags {
		if p := strings.TrimSpace(f); p != "" {
			out = append(out, p)
		}
	}
	if len(out) > 0 {
		return out
	}
	return SplitCorpusEnv(env)
}
