package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

// Hit is one search result as consumed by the harness. Implementations map
// their own result types onto this minimal shape (ID for A/B recall, Text for
// fragment recall, Root for reporting).
type Hit struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Root string `json:"root"`
}

// Searcher runs one golden query and returns up to limit hits. Implementations
// must be safe for concurrent use when the runner uses workers > 1.
type Searcher interface {
	// Search runs the query and returns the ranked hits.
	Search(ctx context.Context, query string, limit int) ([]Hit, error)
	// Close releases any resources (database handle, subprocess).
	Close() error
	// Name identifies the searcher in reports ("http://…", "inproc", "exec:…").
	Name() string
}

// ExecSearcher shells out to a candidate binary that implements the
// bin/brain/search.go CLI contract:
//
//	<path> --json -n <limit> --no-web "<query>"
//
// and emits the search JSON on stdout. Used for --candidate <bin>; the wire
// format is the same shape as the search tool, so an ANN/vector candidate
// built for issue #203+ can be measured without touching the harness.
type ExecSearcher struct {
	Path    string
	Timeout time.Duration
}

// searchOut mirrors the JSON output of bin/brain/search.go (and the HTTP API).
type searchOut struct {
	Query   string `json:"query"`
	Count   int    `json:"count"`
	Results []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
		Root string `json:"root"`
	} `json:"results"`
}

func (s *ExecSearcher) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	if s.Path == "" {
		return nil, fmt.Errorf("exec searcher: empty path")
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.Path, "--json", "-n", strconv.Itoa(limit), "--no-web", query)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if err2, ok := err.(*exec.ExitError); ok {
			exitErr = err2
		} else {
			return nil, fmt.Errorf("exec %s: %w", s.Path, err)
		}
		if exitErr != nil {
			return nil, fmt.Errorf("exec %s: %s", s.Path, trimErr(string(exitErr.Stderr)))
		}
		return nil, err
	}
	var parsed searchOut
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil, fmt.Errorf("exec %s: parse output: %w", s.Path, err)
	}
	hits := make([]Hit, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		hits = append(hits, Hit{ID: r.ID, Text: r.Text, Root: r.Root})
	}
	return hits, nil
}

func (s *ExecSearcher) Close() error { return nil }

func (s *ExecSearcher) Name() string { return "exec:" + s.Path }

func trimErr(s string) string {
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
