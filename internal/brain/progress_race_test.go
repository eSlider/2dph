package brain

import (
	"io"
	"sync"
	"testing"
	"time"
)

// TestProgressReporterConcurrentRace proves the progress reporter is safe to
// call from many concurrent embedding workers (index bulk-rebuild path via
// WriteCorpus → parallelEmbed → progress.Report). Run with -race: before the
// fix, Report/Finish wrote the total field outside the mutex while render read
// it under the mutex, so concurrent workers raced on it.
func TestProgressReporterConcurrentRace(t *testing.T) {
	var wg sync.WaitGroup
	r := NewProgressReporter(io.Discard, time.Hour)
	total := 512
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 32; i++ {
				r.Report(w*32+i+1, total)
			}
		}(w)
	}
	wg.Wait()
	r.Finish(total, total)
}

// TestProgressReporterFinishReadsLastTotal checks Finish renders the final
// total consistently after concurrent Reports (total must converge to the
// last non-zero value passed in).
func TestProgressReporterFinishReadsLastTotal(t *testing.T) {
	r := NewProgressReporter(io.Discard, time.Hour)
	r.Report(1, 10)
	r.Report(2, 20)
	r.Report(3, 30)
	r.Finish(30, 30)
	if got := r.total.Load(); got != 30 {
		t.Fatalf("total = %d, want 30", got)
	}
}
