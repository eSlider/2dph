package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Golden topics cover the corpus areas named in issue #202.
var goldenTopics = map[string]bool{
	"facts": true, "mail": true, "docs": true, "git": true,
	"ssh": true, "mixed": true,
}

var goldenLangs = map[string]bool{"ru": true, "en": true}

// GoldenEntry is one benchmark query. Fragment is the expected evidence: a
// top-k hit must contain every word of it (single word = substring, same as
// the eval.go gate). Queries without a fragment still run (latency, A/B
// recall vs baseline) but are excluded from the fragment recall gate.
type GoldenEntry struct {
	Query    string `json:"q"`
	Topic    string `json:"topic"`
	Lang     string `json:"lang"`
	Fragment string `json:"fragment,omitempty"`
}

// GoldenSet is the on-disk golden-set file format.
type GoldenSet struct {
	Version int           `json:"version"`
	Source  string        `json:"source"`
	Queries []GoldenEntry `json:"queries"`
}

// LoadGolden reads and validates a golden-set file.
func LoadGolden(path string) (*GoldenSet, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read golden: %w", err)
	}
	var g GoldenSet
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, fmt.Errorf("parse golden %s: %w", path, err)
	}
	if err := g.Validate(); err != nil {
		return nil, fmt.Errorf("golden %s: %w", path, err)
	}
	return &g, nil
}

// Validate checks schema invariants: version, non-empty unique queries,
// known topic/lang values. Fragments are optional but must not be empty
// strings when present.
func (g *GoldenSet) Validate() error {
	if g.Version < 1 {
		return fmt.Errorf("version must be >= 1, got %d", g.Version)
	}
	if len(g.Queries) == 0 {
		return fmt.Errorf("no queries")
	}
	seen := make(map[string]bool, len(g.Queries))
	for i, e := range g.Queries {
		q := strings.TrimSpace(e.Query)
		if q == "" {
			return fmt.Errorf("query #%d: empty", i)
		}
		if seen[q] {
			return fmt.Errorf("query #%d: duplicate %q", i, q)
		}
		seen[q] = true
		if !goldenTopics[e.Topic] {
			return fmt.Errorf("query %q: unknown topic %q", q, e.Topic)
		}
		if !goldenLangs[e.Lang] {
			return fmt.Errorf("query %q: unknown lang %q", q, e.Lang)
		}
		if f := strings.TrimSpace(e.Fragment); e.Fragment != "" && f == "" {
			return fmt.Errorf("query %q: fragment is blank", q)
		}
	}
	return nil
}

// FragmentCount returns the number of queries carrying an expected fragment
// (the denominator of the fragment recall gate).
func (g *GoldenSet) FragmentCount() int {
	n := 0
	for _, e := range g.Queries {
		if strings.TrimSpace(e.Fragment) != "" {
			n++
		}
	}
	return n
}
