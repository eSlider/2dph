//usr/bin/env go run -tags=index_loop "$0" "$@"; exit
//go:build index_loop
//
// bin/stack/index-loop.go - compose-managed periodic cycle that closes the
// chain gator sync→etl→pack → 2dph import(graph) → index(kb.lbug) (#292).
//
//	./bin/stack/index-loop.go --interval 1h     # long-running loop (compose service)
//	./bin/stack/index-loop.go --once            # one cycle, then exit (manual control)
//	./bin/stack/index-loop.go --once --hive /gator/parquet/mail
//	./bin/stack/index-loop.go --once --no-quiesce   # brain already stopped
//
// One cycle: discover gator mail channels → quiesce the compose brain
// (Ladybug is single-writer) → incremental index (--skip) → idempotent graph
// import per channel → ANN ensure → restart brain → persist freshness state
// for /stats and scripts/stack/status. systemd is not used; the compose
// service `index-sync` runs this loop under `restart: unless-stopped`.
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/dockerctl"
	"github.com/eSlider/2dph/internal/indexsync"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("index-loop", flag.ExitOnError)
	once := fs.Bool("once", false, "run a single cycle and exit")
	interval := fs.Duration("interval", 0, "loop interval (default 1h)")
	hive := fs.String("hive", "", "gator parquet/mail hive root (default config gator.mailhive / GATOR_MAIL_HIVE)")
	root := fs.String("root", "", "repo root (default: autodetect)")
	db := fs.String("db", "", "kb.lbug path (default <root>/var/kb.lbug)")
	mailGraph := fs.String("mail-graph", "", "mail-graph binary (default: PATH lookup)")
	brainIndex := fs.String("brain-index", "", "brain-index binary (default: PATH lookup)")
	annBin := fs.String("ann", "", "brain-ann binary for ANN ensure (empty = skip)")
	quiesce := fs.Bool("quiesce", true, "stop/start the compose brain around the write")
	noQuiesce := fs.Bool("no-quiesce", false, "do not touch the compose brain (already stopped)")
	rebuild := fs.Bool("rebuild", false, "force a full --rebuild instead of --skip")
	socket := fs.String("docker-socket", "", "Docker socket (default /var/run/docker.sock)")
	project := fs.String("project", "", "compose project label (default 2dph)")
	service := fs.String("service", "", "compose service label (default brain)")
	jsonOut := fs.Bool("json", false, "JSON report")
	_ = fs.Parse(args)

	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "index-loop: config: %v\n", err)
		return 1
	}

	// Precedence: flag → env (container mount) → config (host CLI, #79).
	hiveRoot := *hive
	if hiveRoot == "" {
		hiveRoot = os.Getenv("GATOR_MAIL_HIVE")
	}
	if hiveRoot == "" {
		hiveRoot = cfg.Gator.MailHive
	}
	if hiveRoot == "" {
		fmt.Fprintln(os.Stderr, "index-loop: gator mail hive is not configured: pass --hive, set gator.mailhive, or GATOR_MAIL_HIVE")
		return 1
	}

	ic := indexsync.Config{
		Root:          *root,
		Hive:          hiveRoot,
		DB:            *db,
		MailGraphBin:  *mailGraph,
		BrainIndexBin: *brainIndex,
		AnnBin:        *annBin,
		Interval:      *interval,
		Quiesce:       *quiesce && !*noQuiesce,
		Rebuild:       *rebuild,
		Project:       *project,
		Service:       *service,
	}.WithDefaults()

	var dc *dockerctl.Client
	if ic.Quiesce {
		dc = dockerctl.New(dockerSocket(*socket))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	emit := func(rep indexsync.Report) {
		if *jsonOut {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"started_at": rep.StartedAt.Format(time.RFC3339),
				"skipped":    rep.Skipped, "reason": rep.Reason,
				"channels": rep.Channels, "imported": rep.Imported,
				"indexed": rep.Indexed, "ann": rep.ANN, "error": rep.Err,
			})
			return
		}
		fmt.Fprintln(os.Stderr, rep.Summary())
	}

	if *once {
		rep, err := indexsync.Cycle(ctx, ic, dc)
		emit(rep)
		if err != nil {
			return 1
		}
		return 0
	}

	fmt.Fprintf(os.Stderr, "index-loop: interval=%s hive=%s db=%s\n", ic.Interval, ic.Hive, ic.DB)
	err = indexsync.Run(ctx, ic, dc, emit)
	if err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "index-loop: %v\n", err)
		return 1
	}
	return 0
}

// dockerSocket resolves the socket: --docker-socket, else DOCKER_HOST
// (unix://...), else the Docker default.
func dockerSocket(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	if h := os.Getenv("DOCKER_HOST"); strings.HasPrefix(h, "unix://") {
		return strings.TrimPrefix(h, "unix://")
	}
	return dockerctl.DefaultSocket
}
