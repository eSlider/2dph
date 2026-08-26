package bench

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Runner executes a golden-set through one Searcher. It is safe for the
// searcher to be used concurrently when Workers > 1 (bounded pool).
type Runner struct {
	Golden   *GoldenSet
	Searcher Searcher
	// Workers caps concurrent searches (1 = sequential, clean per-query
	// latency; >1 measures the server under load).
	Workers int
	// Limit is the top-k hits requested per query (recall@k needs k).
	Limit int
	// ProbePID is the process sampled for CPU/RSS (0 = the runner's own
	// process; a serve daemon's host pid is visible on the same /proc).
	ProbePID int
}

// QueryResult is the outcome of one golden query.
type QueryResult struct {
	Index   int
	Entry   GoldenEntry
	Hits    []Hit
	Elapsed time.Duration
	Err     error
}

// RunReport is one pass (baseline or candidate) over the golden-set.
type RunReport struct {
	Pass        string              `json:"pass"` // "baseline" | "candidate"
	Searcher    string              `json:"searcher"`
	Queries     int                 `json:"queries"`
	Failed      int                 `json:"failed"`
	Latency     Latency             `json:"latency_ms"`
	Recall      RecallResult        `json:"recall_fragment5"` // fragment gate @5
	Recall10    RecallResult        `json:"recall_fragment10"`
	Resources   Resources           `json:"resources"`
	PerQuery    []QueryResult       `json:"per_query"`
	BaselineIDs map[string][]string `json:"-"`
}

// Run executes the golden-set and returns per-query results in golden order
// together with latency/resource aggregates. Per-query errors are collected,
// not fatal: the harness keeps going so one bad query cannot void a run.
func (r *Runner) Run(ctx context.Context) (*RunReport, error) {
	if r.Golden == nil || len(r.Golden.Queries) == 0 {
		return nil, fmt.Errorf("runner: empty golden set")
	}
	if r.Searcher == nil {
		return nil, fmt.Errorf("runner: nil searcher")
	}
	workers := r.Workers
	if workers < 1 {
		workers = 1
	}
	limit := r.Limit
	if limit < 1 {
		limit = 10
	}

	before, err := SampleProc(r.ProbePID)
	if err != nil {
		return nil, fmt.Errorf("sample before: %w", err)
	}
	start := time.Now()

	n := len(r.Golden.Queries)
	results := make([]QueryResult, n)
	ms := make([]float64, 0, n)

	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				entry := r.Golden.Queries[i]
				t0 := time.Now()
				hits, err := r.Searcher.Search(ctx, entry.Query, limit)
				results[i] = QueryResult{
					Index: i, Entry: entry, Hits: hits,
					Elapsed: time.Since(t0), Err: err,
				}
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	after, err := SampleProc(r.ProbePID)
	if err != nil {
		return nil, fmt.Errorf("sample after: %w", err)
	}
	_ = start // wall duration is the sum of per-query latencies (workers=1)

	failed := 0
	for _, res := range results {
		if res.Err != nil {
			failed++
			continue
		}
		ms = append(ms, float64(res.Elapsed.Microseconds())/1000.0)
	}

	return &RunReport{
		Pass:      "",
		Searcher:  r.Searcher.Name(),
		Queries:   n,
		Failed:    failed,
		Latency:   ComputeLatency(ms),
		Recall:    FragmentRecall(results, 5),
		Recall10:  FragmentRecall(results, 10),
		Resources: before.Resources(after),
		PerQuery:  results,
	}, nil
}
