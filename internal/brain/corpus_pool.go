// Parallel embedding and batching helpers for corpus writes. Kept cgo-free so
// they unit-test without the Ladybug library.
package brain

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eSlider/2dph/internal/contract"
)

// poolItem is one unit of parallel work: embed text for item at index i.
type poolItem struct {
	i    int
	text string
}

// embedResult holds the outcome for one poolItem, indexed by item.i.
type embedResult struct {
	i   int
	emb []float64
	err error
}

// parallelEmbed runs embed over items with a worker pool, writing each result
// to its index slot so output order always matches input order. Embed errors
// are collected per-result (not fatal). progress, if set, is called once per
// completion with (done, total). ctx cancellation stops feeding new work and
// returns context.Canceled.
func parallelEmbed(ctx context.Context, items []poolItem, embed func(string) ([]float64, error), workers int, progress func(done, total int)) ([]embedResult, error) {
	if workers <= 0 {
		workers = 1
	}
	results := make([]embedResult, len(items))
	if len(items) == 0 {
		return results, nil
	}
	jobs := make(chan poolItem)
	var done atomic.Int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range jobs {
				emb, err := embed(it.text)
				results[it.i] = embedResult{i: it.i, emb: emb, err: err}
				if progress != nil {
					progress(int(done.Add(1)), len(items))
				}
			}
		}()
	}
	for i, it := range items {
		select {
		case jobs <- poolItem{i: i, text: it.text}:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return results, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return results, nil
}

// filterExistingLeafs drops leafs whose deterministic id (ContentHash) is
// already in existing (resume path). Text is normalized the same way the
// writer normalizes it before hashing, so ids match what the DB stores.
func filterExistingLeafs(leafs []contract.Leaf, existing map[string]bool) []contract.Leaf {
	kept := leafs[:0]
	for _, lf := range leafs {
		lf.Text = contract.NormalizeText(lf.Text)
		if existing[lf.ContentHash()] {
			continue
		}
		kept = append(kept, lf)
	}
	return kept
}

// progressReport — сквозной прогресс чанкованной записи (issue #237): done
// сдвигается на base (уже записанные до этого чанка leafs), total остаётся
// общим по всем чанкам (не размером чанка).
func progressReport(p *ProgressReporter, base, total, done int) {
	if p == nil {
		return
	}
	p.Report(base+done, total)
}

// chunkBounds splits [0,n) into [start,end) batches of at most size. size<=0
// means a single batch.
func chunkBounds(n, size int) [][2]int {
	if size <= 0 {
		size = n
	}
	if n <= 0 || size <= 0 {
		return nil
	}
	var out [][2]int
	for s := 0; s < n; s += size {
		e := s + size
		if e > n {
			e = n
		}
		out = append(out, [2]int{s, e})
	}
	return out
}

// ProgressReporter prints a human progress line (done, rate, ETA) to w at most
// once per interval. Thread-safe; zero value is ready to use.
type ProgressReporter struct {
	interval time.Duration
	emit     func(string)
	last     atomic.Int64 // unix nanos of last emit
	done     atomic.Int64
	total    atomic.Int64
	start    time.Time
	mu       sync.Mutex
}

// NewProgressReporter returns a reporter that prints to w every interval.
func NewProgressReporter(w io.Writer, interval time.Duration) *ProgressReporter {
	return &ProgressReporter{interval: interval, emit: func(s string) { fmt.Fprintf(w, "%s\n", s) }, start: time.Now()}
}

// Report records one completion and prints a line if the interval elapsed.
func (r *ProgressReporter) Report(done, total int) {
	r.done.Store(int64(done))
	if total > 0 {
		r.total.Store(int64(total))
	}
	now := time.Now()
	if r.interval <= 0 {
		return
	}
	if r.last.Load() != 0 && now.Sub(time.Unix(0, r.last.Load())) < r.interval {
		return
	}
	r.last.Store(now.UnixNano())
	r.mu.Lock()
	defer r.mu.Unlock()
	r.render(now, done)
}

// Finish prints a final line regardless of interval.
func (r *ProgressReporter) Finish(done, total int) {
	r.done.Store(int64(done))
	r.total.Store(int64(total))
	r.render(time.Now(), done)
}

func (r *ProgressReporter) render(now time.Time, done int) {
	if r.emit == nil {
		return
	}
	elapsed := now.Sub(r.start).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	rate := float64(done) / elapsed
	var eta string
	remaining := int(r.total.Load()) - done
	if done > 0 && remaining > 0 {
		secs := time.Duration(float64(remaining)/rate) * time.Second
		eta = fmt.Sprintf(" eta=%s", secs.Round(time.Second))
	}
	r.emit(fmt.Sprintf("index: %d/%d %.0f/s%s", done, r.total.Load(), rate, eta))
}
