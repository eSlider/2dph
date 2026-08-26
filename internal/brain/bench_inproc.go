//go:build cgo && system_ladybug

package brain

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/eSlider/2dph/internal/brain/bench"
	"github.com/eSlider/2dph/internal/config"
)

// MainBench is the bin/brain/bench.go entry. It also answers the /embed
// daemon "serve" subcommand: searchHits embeds via the daemon, and the daemon
// is spawned from os.Executable() (the bench binary), so the bench binary
// must serve like bin/brain/search.go does.
func MainBench(args []string) int {
	if len(args) > 0 && args[0] == "serve" {
		port := defaultPort
		if len(args) > 1 {
			if p, err := strconv.Atoi(args[1]); err == nil {
				port = p
			}
		}
		if err := serve(port); err != nil {
			log.Printf("brain/bench serve: %v", err)
			return 1
		}
		return 0
	}
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/bench: config: %v\n", err)
		return 1
	}
	Configure(cfg)
	// --candidate inproc-ann: same process, same DB handle, vector path
	// forced through the ANN index — the A/B isolates the vector layer
	// (baseline = force-off linear scan, candidate = force-on ANN). Pairs
	// with --inproc (quiesced DB); opens the local handle if the baseline
	// ran against a remote brain instead.
	bench.CandInprocOpener = func(_ context.Context, dbPath string) (bench.Searcher, error) {
		if dbPath != "" {
			prev := dbPathFn
			dbPathFn = func() string { return dbPath }
			defer func() { dbPathFn = prev }()
		}
		if !brainOpen() {
			if err := openBrain(); err != nil {
				return nil, fmt.Errorf("open brain: %w", err)
			}
		}
		setAnnMode(annForceOn)
		return &inprocSearcher{}, nil
	}
	return bench.Main(args, func(_ context.Context, dbPath string) (bench.Searcher, error) {
		if dbPath != "" {
			prev := dbPathFn
			dbPathFn = func() string { return dbPath }
			defer func() { dbPathFn = prev }()
		}
		if err := openBrain(); err != nil {
			return nil, fmt.Errorf("open brain: %w", err)
		}
		// Baseline must be the linear scan even when the config enables ANN.
		setAnnMode(annForceOff)
		return &inprocSearcher{}, nil
	})
}

// inprocSearcher runs the real search pipeline (embed → FTS → vector scan →
// rank) against the locally opened read handle. Read-only: takes brainMu.RLock
// per query and never writes. Requires the serve process (or any other holder
// of kb.lbug) to be quiesced first — same single-writer rule as issue #202.
type inprocSearcher struct{}

func (s *inprocSearcher) Search(_ context.Context, query string, limit int) ([]bench.Hit, error) {
	hits, err := searchHits(query, "", "", limit, "", false, false)
	if err != nil {
		return nil, err
	}
	out := make([]bench.Hit, 0, len(hits))
	for _, h := range hits {
		out = append(out, bench.Hit{ID: h.ID, Text: h.Text, Root: h.Root})
	}
	return out, nil
}

func (s *inprocSearcher) Close() error {
	closeBrain()
	return nil
}

func (s *inprocSearcher) Name() string { return "inproc" }
