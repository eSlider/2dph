// Package runner wires the bounded sync-ETL pipeline (#98):
//
//	Source → Decode(Registry) → Transform → Load
//
// The stages are connected by bounded channels, so a slow consumer applies
// backpressure to the producer instead of buffering unboundedly. The pipeline
// runs under errgroup.WithContext: the first stage error cancels every other
// stage, and a context cancellation drains in-flight work and returns cleanly.
//
// The driver reuses source.Sync (#97) for fetch + sha256 seen-set dedup +
// atomic checkpoints; the pipeline's decode stage resolves handlers through the
// etl.Registry (#96) and calls them concurrently.
package runner

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/eSlider/2dph/internal/etl"
	"github.com/eSlider/2dph/internal/source"
)

// Item is one unit of work flowing through the pipeline stages. Blob is set by
// the source producer; Kind selects the registry handler (defaults to
// Blob.Kind); Out is filled by the transform stage and consumed by the load.
type Item struct {
	Blob source.Blob
	Kind string
	Out  string
}

// TransformFn maps one decoded item to its output form. Optional; when nil the
// item passes through with an empty Out.
type TransformFn func(ctx context.Context, it Item) (string, error)

// LoadFn persists one finished item. Optional; when nil the load stage only
// counts the item. It is called by a single load stage, so it must not block
// on the pipeline's own channels.
type LoadFn func(ctx context.Context, it Item) error

// Config configures a pipeline run.
type Config struct {
	// Source is the sync-ETL adapter feeding the pipeline (required).
	Source source.Source
	// Registry resolves each blob's kind to a handler (required).
	Registry *etl.Registry
	// StatePath is the source checkpoint file (var/state/<Name>.json). Required.
	StatePath string
	// StatsPath is the YAML report path; empty = no file written.
	StatsPath string
	// Workers is the decode+transform concurrency. Non-positive ⇒ GOMAXPROCS.
	Workers int
	// Buffer is the bounded channel capacity between stages. Non-positive ⇒ 8.
	Buffer int
	// Transform transforms each decoded item (optional).
	Transform TransformFn
	// Load sinks each finished item (optional).
	Load LoadFn
}

// Run drives one pipeline pass: source producer → bounded worker pool
// (decode+transform) → load sink. It returns the per-run report and the first
// stage error (nil on a clean drain).
func Run(ctx context.Context, cfg Config) (*Report, error) {
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	start := time.Now()
	stats := newHandlerStats()

	g, gctx := errgroup.WithContext(ctx)
	in := make(chan Item, cfg.Buffer)
	out := make(chan Item, cfg.Buffer)

	// Stage Load: single consumer draining `out` to the sink.
	g.Go(func() error { return loadStage(gctx, out, cfg.Load) })

	// Stages Decode+Transform: bounded worker pool, fan-out from `in`,
	// fan-in to `out`. `workers` counts them so a separate errgroup goroutine
	// can close `out` after the last worker exits (success or cancel).
	var workers sync.WaitGroup
	workers.Add(cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		g.Go(func() error {
			defer workers.Done()
			return processStage(gctx, in, out, cfg, stats)
		})
	}
	g.Go(func() error {
		workers.Wait()
		close(out)
		return nil
	})

	// Stage Source: producer driving source.Sync. The handle blocks on the
	// bounded `in` channel, which is the backpressure point: a slow pipeline
	// throttles the fetch loop instead of buffering unboundedly.
	var srcStats source.Stats
	g.Go(func() error {
		defer close(in)
		st, err := source.Sync(gctx, cfg.Source, func(c context.Context, b source.Blob) error {
			select {
			case in <- Item{Blob: b, Kind: b.Kind}:
				return nil
			case <-gctx.Done():
				return gctx.Err()
			}
		}, source.Options{StatePath: cfg.StatePath})
		srcStats = st
		return err
	})

	err := g.Wait()
	rep := &Report{
		StartedAt: start,
		Duration:  time.Since(start),
		Source:    srcStats,
		Handlers:  stats.snapshot(),
	}
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		rep.Error = err.Error()
	}
	if cfg.StatsPath != "" {
		if werr := WriteYAML(cfg.StatsPath, rep); werr != nil && err == nil {
			return nil, werr
		}
	}
	return rep, err
}

// withDefaults fills zero-valued concurrency knobs with sane defaults.
func (c Config) withDefaults() Config {
	if c.Workers <= 0 {
		c.Workers = runtime.GOMAXPROCS(0)
	}
	if c.Buffer <= 0 {
		c.Buffer = 8
	}
	return c
}

func (c Config) validate() error {
	switch {
	case c.Source == nil:
		return errors.New("runner: Source is required")
	case c.Registry == nil:
		return errors.New("runner: Registry is required")
	case c.StatePath == "":
		return errors.New("runner: StatePath is required")
	}
	return nil
}

// processStage resolves each item's handler through the Registry (decode),
// runs the optional transform, and forwards the item to the load stage.
func processStage(ctx context.Context, in <-chan Item, out chan<- Item, cfg Config, stats *handlerStats) error {
	for {
		select {
		case it, ok := <-in:
			if !ok {
				return nil
			}
			if err := decode(ctx, it, cfg); err != nil {
				stats.failed(it.Kind)
				return fmt.Errorf("runner: decode %s: %w", it.Blob.ID, err)
			}
			stats.ok(it.Kind)
			if cfg.Transform != nil {
				o, err := cfg.Transform(ctx, it)
				if err != nil {
					stats.failed(it.Kind)
					return fmt.Errorf("runner: transform %s: %w", it.Blob.ID, err)
				}
				it.Out = o
			}
			select {
			case out <- it:
			case <-ctx.Done():
				return ctx.Err()
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// decode looks up the handler for the item's kind and runs it on the blob path.
func decode(ctx context.Context, it Item, cfg Config) error {
	key := it.Kind
	if key == "" {
		key = "default"
	}
	h, ok := cfg.Registry.Lookup(key)
	if !ok {
		return fmt.Errorf("runner: no handler for kind %q", key)
	}
	return h.Handle(ctx, it.Blob.Path)
}

// loadStage is the single sink consumer of `out`. It drains until `out` is
// closed or the context is cancelled, forwarding each item to the sink.
func loadStage(ctx context.Context, out <-chan Item, load LoadFn) error {
	for {
		select {
		case it, ok := <-out:
			if !ok {
				return nil
			}
			if load != nil {
				if err := load(ctx, it); err != nil {
					return fmt.Errorf("runner: load: %w", err)
				}
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
