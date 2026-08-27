//usr/bin/env go run "$0" "$@"; exit
//
// bin/cron/browser-sync.go - periodic browser extraction → brain ingest.
//
//	./bin/cron/browser-sync.go                  # one cycle, default paths
//	./bin/cron/browser-sync.go --interval 6h    # run continuously
//	./bin/cron/browser-sync.go --skip-extract   # no Thorium probe
//	./bin/cron/browser-sync.go --root /srv/2dph # explicit repo root
//
// Probes Thorium CDP via the agent-browser CLI, reads var/corpus/{gmail,linkedin,
// djinni}/*.json, converts entries to brain leafs and POSTs them to <brain>/ingest.
// A down Thorium is tolerated: extraction is skipped and the last-known corpus is
// still ingested. Each cycle is appended to var/log/browser-sync.log. Scheduled
// every 6h by scripts/browser-sync.{service,timer} (install: scripts/browser-sync-install.sh).
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/eSlider/2dph/internal/cron"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("browser-sync", flag.ExitOnError)
	root := fs.String("root", "", "repo root (default: autodetect)")
	corpus := fs.String("corpus", "", "corpus dir (default <root>/var/corpus)")
	logPath := fs.String("log", "", "log file (default <root>/var/log/browser-sync.log)")
	brainURL := fs.String("brain", "", "brain base URL (default http://127.0.0.1:8630)")
	agentBin := fs.String("agent-bin", "", "agent-browser CLI (default: agent-browser)")
	cdpPort := fs.String("cdp-port", "9222", "Thorium CDP port")
	skipExtract := fs.Bool("skip-extract", false, "skip the Thorium extraction probe")
	interval := fs.Duration("interval", 0, "run continuously every INTERVAL; 0 = one shot")
	timeout := fs.Duration("timeout", 10*time.Minute, "per-ingest POST timeout")
	_ = fs.Parse(args)

	cfg := cron.Config{
		Root:        *root,
		Corpus:      *corpus,
		LogPath:     *logPath,
		Brain:       *brainURL,
		AgentBin:    *agentBin,
		CDPPort:     *cdpPort,
		SkipExtract: *skipExtract,
		Timeout:     *timeout,
	}.WithDefaults()

	logger, closer := fileLogger(cfg.LogPath)
	defer closer()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	for {
		rep, err := cron.RunOnce(ctx, cfg)
		line := rep.Summary()
		if err != nil {
			line += " err=" + err.Error()
		}
		logger.Println(line)

		if err != nil && ctx.Err() == nil {
			// A hard corpus/brain failure is retried on the next timer tick;
			// exit non-zero so systemd can flag it.
			return 1
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

// fileLogger appends to path (creating the dir and file as needed) and also
// mirrors to stderr; falls back to stderr only when the file cannot be opened.
func fileLogger(path string) (*log.Logger, func()) {
	if path == "" {
		return log.New(os.Stderr, "", log.LstdFlags), func() {}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return log.New(os.Stderr, "", log.LstdFlags), func() {}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return log.New(os.Stderr, "", log.LstdFlags), func() {}
	}
	l := log.New(io.MultiWriter(f, os.Stderr), "", log.LstdFlags)
	return l, func() { _ = f.Close() }
}
