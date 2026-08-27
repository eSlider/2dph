package bench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/pkg/cli"
)

// DefaultGolden is the committed golden-set, relative to the repo root
// (run the tool from the repo root, like every other bin/brain tool).
const DefaultGolden = "internal/brain/testdata/golden-set.json"

// Default gate thresholds (issue #201/#202).
const (
	DefaultMinRecall5      = 0.95
	DefaultMaxLatencyRatio = 1.5
	DefaultURL             = "http://127.0.0.1:8630"
)

// InprocOpener opens the in-process (local DB) searcher; wired by the cgo
// brain package so this entry stays cgo-free and testable. dbPath is the
// --db flag (empty = the default var/kb.lbug).
type InprocOpener func(ctx context.Context, dbPath string) (Searcher, error)

// CandInprocOpener opens the in-process ANN candidate for --candidate
// inproc-ann (issue #204): same DB handle, vector path forced through the
// ANN index so the A/B isolates the vector layer. Wired by the cgo brain
// package; nil = mode unsupported.
var CandInprocOpener InprocOpener

type options struct {
	baseline  bool
	candidate string
	golden    string
	jsonOut   bool
	workers   int
	url       string
	inproc    bool
	dbPath    string
	limit     int
	pid       int
	minRecall float64
	maxRatio  float64
}

// Main is the bin/brain/bench.go entry: run the golden-set through the
// baseline (and, with --candidate, through a candidate), print table or JSON,
// apply the gates. Exit codes: 0 pass, 1 runtime error, 2 gate failure.
func Main(args []string, openInproc InprocOpener) int {
	opt, err := parse(args)
	if err != nil {
		if errors.Is(err, cli.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "brain/bench: %v\n", err)
		return 2
	}

	golden, err := LoadGolden(opt.golden)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/bench: %v\n", err)
		return 1
	}

	ctx := context.Background()
	base, err := newBaseline(opt, openInproc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/bench: %v\n", err)
		return 1
	}
	defer base.Close()

	run := &Runner{
		Golden: golden, Searcher: base, Workers: opt.workers,
		Limit: opt.limit, ProbePID: opt.pid,
	}
	baseReport, err := run.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/bench: baseline: %v\n", err)
		return 1
	}
	baseReport.Pass = "baseline"
	// Persist baseline top-k IDs as truth for the A/B comparison.
	baseReport.BaselineIDs = make(map[string][]string, len(baseReport.PerQuery))
	for _, q := range baseReport.PerQuery {
		if q.Err == nil {
			baseReport.BaselineIDs[q.Entry.Query] = TopIDs(q.Hits, opt.limit)
		}
	}

	rep := &Report{
		Tool: "brain/bench", Golden: opt.golden,
		Workers: opt.workers, Limit: opt.limit,
		Baseline: baseReport,
	}
	rep.Gates.Recall5 = GateResult{
		Name: "recall@5", Threshold: opt.minRecall,
		Value:  baseReport.Recall.Score,
		Passed: baseReport.Recall.Score >= opt.minRecall,
	}

	if opt.candidate != "" {
		var (
			cand Searcher
			err  error
		)
		if opt.candidate == "inproc-ann" {
			if CandInprocOpener == nil {
				fmt.Fprintf(os.Stderr, "brain/bench: --candidate inproc-ann needs the ladybug build (bin/cgo/zig go run -tags=system_ladybug,brain_bench)\n")
				return 1
			}
			cand, err = CandInprocOpener(context.Background(), opt.dbPath)
		} else {
			cand, err = newCandidate(opt.candidate)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/bench: %v\n", err)
			return 1
		}
		defer cand.Close()
		candRun := &Runner{
			Golden: golden, Searcher: cand, Workers: opt.workers,
			Limit: opt.limit, ProbePID: opt.pid,
		}
		candReport, err := candRun.Run(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/bench: candidate: %v\n", err)
			return 1
		}
		candReport.Pass = "candidate"
		rep.Candidate = candReport

		// A/B regression: candidate must not lose baseline top-k hits.
		recalled, total := 0, 0
		for _, q := range candReport.PerQuery {
			truth, ok := baseReport.BaselineIDs[q.Entry.Query]
			if !ok || q.Err != nil {
				continue
			}
			total++
			if allIn(truth, q.Hits, opt.limit) {
				recalled++
			}
		}
		score := 0.0
		if total > 0 {
			score = round3(float64(recalled) / float64(total))
		}
		rep.Gates.CandidateRecall = &GateResult{
			Name: "candidate recall@5 vs baseline", Threshold: opt.minRecall,
			Value: score, Passed: score >= opt.minRecall,
		}

		ratio := 0.0
		if baseReport.Latency.P50 > 0 && candReport.Latency.P50 > 0 {
			ratio = round3(candReport.Latency.P50 / baseReport.Latency.P50)
		}
		rep.Gates.LatencyRatio = &GateResult{
			Name: "latency p50 ratio", Threshold: opt.maxRatio,
			Value: ratio, Passed: opt.maxRatio <= 0 || ratio <= opt.maxRatio,
		}
	}

	if opt.jsonOut {
		data, err := rep.JSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/bench: json: %v\n", err)
			return 1
		}
		fmt.Println(string(data))
	} else {
		fmt.Print(rep.Table())
	}
	if !rep.Gates.Recall5.Passed {
		return 2
	}
	if rep.Gates.CandidateRecall != nil && !rep.Gates.CandidateRecall.Passed {
		return 2
	}
	if rep.Gates.LatencyRatio != nil && !rep.Gates.LatencyRatio.Passed {
		return 2
	}
	return 0
}

func parse(args []string) (options, error) {
	var opt options
	p := cli.New("brain-bench")
	p.Description = "A/B benchmark of brain search (golden-set latency/recall/resources)"
	p.Bool(&opt.baseline, "", "baseline", "run the current scan (default mode)")
	p.String(&opt.candidate, "", "candidate", "candidate searcher: http(s) URL, executable path, or inproc-ann (same-process ANN)")
	p.String(&opt.golden, "", "golden", "golden-set file (default internal/brain/testdata/golden-set.json)")
	p.Bool(&opt.jsonOut, "", "json", "JSON output")
	p.Int(&opt.workers, "", "workers", "parallel queries (default 1)")
	p.String(&opt.url, "", "url", "brain MCP base URL (default http://127.0.0.1:8630)")
	p.Bool(&opt.inproc, "", "inproc", "open the local brain DB read-only in-process (quiesce serve first)")
	p.String(&opt.dbPath, "", "db", "kb.lbug path for --inproc (default: var/kb.lbug under KB_ROOT)")
	p.Int(&opt.limit, "", "limit", "top-k hits per query (default 10)")
	p.Int(&opt.pid, "", "pid", "sample CPU/RSS of this pid (default: own process)")
	p.Float64(&opt.minRecall, "", "min-recall5", "recall@5 gate (default 0.95)")
	p.Float64(&opt.maxRatio, "", "max-ratio", "candidate/baseline p50 latency gate (default 1.5)")
	if err := cli.Parse(p, args); err != nil {
		return opt, err
	}
	if opt.golden == "" {
		opt.golden = DefaultGolden
	}
	if opt.workers < 1 {
		opt.workers = 1
	}
	if opt.limit < 1 {
		opt.limit = 10
	}
	if opt.minRecall <= 0 {
		opt.minRecall = DefaultMinRecall5
	}
	if opt.maxRatio <= 0 {
		opt.maxRatio = DefaultMaxLatencyRatio
	}
	return opt, nil
}

func newBaseline(opt options, openInproc InprocOpener) (Searcher, error) {
	if opt.inproc {
		if openInproc == nil {
			return nil, fmt.Errorf("--inproc needs the ladybug build (bin/cgo/zig go run -tags=system_ladybug,brain_bench)")
		}
		return openInproc(context.Background(), opt.dbPath)
	}
	url := opt.url
	if url == "" {
		url = DefaultURL
	}
	return NewHTTPSearcher(HTTPConfig{BaseURL: url}), nil
}

func newCandidate(path string) (Searcher, error) {
	if path == "" {
		return nil, fmt.Errorf("empty --candidate")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return NewHTTPSearcher(HTTPConfig{BaseURL: path}), nil
	}
	if st, err := os.Stat(path); err == nil && !st.IsDir() && st.Mode()&0111 != 0 {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		return &ExecSearcher{Path: abs}, nil
	}
	return nil, fmt.Errorf("unsupported candidate %q: use an http(s) URL or an executable path (index candidates land with #203)", path)
}

// allIn reports whether every baseline truth ID appears in the candidate's
// top-k hits.
func allIn(truth []string, hits []Hit, k int) bool {
	if k > len(truth) {
		k = len(truth)
	}
	seen := make(map[string]bool, len(hits))
	for _, h := range hits {
		if len(seen) >= k {
			break
		}
		seen[h.ID] = true
	}
	for i := 0; i < k; i++ {
		if !seen[truth[i]] {
			return false
		}
	}
	return true
}
