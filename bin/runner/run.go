// usr/bin/env go run "$0" "$@"; exit
//
// bin/runner/run.go - bounded sync-ETL pipeline runner (#98).
//
//	./bin/runner/run.go --corpus var/corpus/mail --stats var/state/runner.yml
//	./bin/runner/run.go --corpus var/corpus/mail --interval 300   # daemon loop
//	./bin/runner/run.go --workers 4 --buffer 8
//
// Drives Source → Decode(Registry) → Transform → Load with bounded channels
// between stages (backpressure), a per-run stats YAML, and graceful shutdown
// on SIGINT/SIGTERM. Default source is the disk corpus (.eml); the registry
// resolves each .eml to the mailconv handler. Re-runs are idempotent via the
// source checkpoint seen-set.
//
// NOTE: never run `gofmt -w` on this file - it rewrites the shebang.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/etl"
	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/eSlider/2dph/internal/runner"
	"github.com/eSlider/2dph/internal/source"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("runner", flag.ExitOnError)
	root := fs.String("root", "", "repo root (default: autodetect via config)")
	corpus := fs.String("corpus", "", "corpus dir with .eml files (default <root>/var/corpus/mail)")
	state := fs.String("state", "", "source checkpoint file (default <root>/var/state/disk.json)")
	stats := fs.String("stats", "", "stats YAML path (default <root>/var/state/runner.yml)")
	workers := fs.Int("workers", 0, "decode+transform concurrency (default GOMAXPROCS)")
	buffer := fs.Int("buffer", 0, "bounded channel capacity per stage (default 8)")
	interval := fs.Duration("interval", 0, "run continuously every INTERVAL; 0 = one shot")
	_ = fs.Parse(args)

	// Config via the go-config stack (paths come from config, not hardcoded).
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "runner:", err)
		return 1
	}
	base := *root
	if base == "" {
		base = cfg.Root
	}
	if base == "" {
		base, _ = os.Getwd()
	}
	if *corpus == "" {
		*corpus = filepath.Join(base, "var", "corpus", "mail")
	}
	if *state == "" {
		*state = filepath.Join(base, "var", "state", "disk.json")
	}
	if *stats == "" {
		*stats = filepath.Join(base, "var", "state", "runner.yml")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	for {
		rep, err := runner.Run(ctx, runner.Config{
			Source:    &source.Disk{Root: *corpus},
			Registry:  mailRegistry(),
			StatePath: *state,
			StatsPath: *stats,
			Workers:   *workers,
			Buffer:    *buffer,
		})
		if err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "runner:", err)
			return 1
		}
		if rep != nil {
			fmt.Printf("runner: new=%d skipped=%d handlers=%d\n",
				rep.Source.New, rep.Source.Skipped, len(rep.Handlers))
		}
		if *interval <= 0 || ctx.Err() != nil {
			return 0
		}
		select {
		case <-time.After(*interval):
		case <-ctx.Done():
			return 0
		}
	}
}

// mailRegistry registers the per-format handlers the runner dispatches to.
// Currently one handler: "mail" converts a raw .eml to markdown (#98). Other
// kinds (git/chat/...) register here as their handlers land.
func mailRegistry() *etl.Registry {
	reg := etl.NewRegistry()
	_ = reg.Register(mailHandler{})
	return reg
}

// mailHandler is the ETL handler for raw .eml mail blobs.
type mailHandler struct{}

func (mailHandler) Name() string { return "mail" }

func (mailHandler) Handle(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return mailconv.FromEMLFile(path, false)
}
