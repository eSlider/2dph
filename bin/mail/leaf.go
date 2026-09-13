//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,mail_graph "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && mail_graph
//
// bin/mail/leaf.go - import gator kind=mail parquet into searchable Leaf nodes
// (ADR-0013, issue #297). Built with gcc (DuckDB static lib), same as mail-graph.
//
//	./bin/mail/leaf.go --dry-run --hive /gator/parquet/mail
//	./bin/mail/leaf.go --commit --skip --hive /gator/parquet/mail
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/corpus"
	cliparse "github.com/eSlider/2dph/pkg/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

type leafFlags struct {
	hive, db, since string
	dryRun, commit  bool
	skip, force     bool
	jsonOut         bool
	chunk, workers, batch int
}

func gitRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func hiveRoot(cfg *config.Config, flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if cfg.Gator.MailHive != "" {
		return cfg.Gator.MailHive, nil
	}
	if v := os.Getenv("GATOR_MAIL_HIVE"); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("gator mail hive root is not configured: pass --hive, set config gator.mailhive, or GATOR_MAIL_HIVE")
}

func run(args []string) int {
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail-leaf: config: %v\n", err)
		return 1
	}
	brain.Configure(cfg)

	v := leafFlags{dryRun: true}
	p := cliparse.New("mail-leaf")
	p.String(&v.hive, "", "hive", "gator parquet/mail hive root")
	p.String(&v.db, "", "db", "path to kb.lbug")
	p.String(&v.since, "", "since", "only messages >= YYYY-MM-DD")
	p.Bool(&v.dryRun, "", "dry-run", "count leafs, write nothing (default)")
	p.Bool(&v.commit, "", "commit", "write Leaf nodes into kb.lbug")
	p.Bool(&v.skip, "", "skip", "skip leafs already in the db (resume)")
	p.Bool(&v.force, "", "force", "write even if the db is open by a live process")
	p.Bool(&v.jsonOut, "", "json", "JSON result")
	p.Int(&v.chunk, "", "chunk", "leafs per chunk before write (default 2048)")
	p.Int(&v.workers, "", "workers", "parallel embedding workers (default 4)")
	p.Int(&v.batch, "", "batch", "leafs per transaction (default 64)")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	if v.commit {
		v.dryRun = false
	}

	hive, err := hiveRoot(cfg, v.hive)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail-leaf:", err)
		return 1
	}

	src := corpus.GatorMail{Hive: hive, Since: v.since}
	ctx := context.Background()
	start := time.Now()

	if v.dryRun {
		n := 0
		if err := src.Stream(ctx, func(contract.Leaf) error { n++; return nil }); err != nil {
			fmt.Fprintf(os.Stderr, "mail-leaf: stream: %v\n", err)
			return 1
		}
		res := map[string]any{"hive": hive, "leafs": n, "read_ms": time.Since(start).Milliseconds()}
		if v.jsonOut {
			_ = json.NewEncoder(os.Stdout).Encode(res)
		} else {
			fmt.Printf("mail-leaf: dry-run hive=%s: %d leafs (%dms)\n", hive, n, res["read_ms"])
		}
		return 0
	}

	root := cfg.Root
	if root == "" {
		root = gitRepoRoot()
	}
	dbpath := v.db
	if dbpath == "" {
		dbpath = filepath.Join(root, "var", "kb.lbug")
	}
	if err := ensureWritable(dbpath, root, cfg, v.force); err != nil {
		fmt.Fprintf(os.Stderr, "mail-leaf: %v\n", err)
		return 2
	}

	var model *brain.StaticModel
	if !cfg.Vector.ANN.Enabled {
		model, err = brain.LoadModel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mail-leaf: model: %v\n", err)
			return 1
		}
		defer model.Close()
	}

	db, conn, err := brain.OpenWritable(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail-leaf: open %s: %v\n", dbpath, err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "mail-leaf: schema: %v\n", err)
		return 1
	}

	var existing map[string]bool
	if v.skip {
		if existing, err = brain.ExistingLeafIDSet(conn); err != nil {
			fmt.Fprintf(os.Stderr, "mail-leaf: resume set: %v\n", err)
			return 1
		}
	}

	stats := brain.CorpusStats{}
	written, err := brain.WriteCorpusChunked(ctx, []contract.Source{src}, v.chunk, 0, stats, func(chunk []contract.Leaf, base, total int) (int, error) {
		return brain.WriteCorpus(conn, chunk, model, brain.WriteOptions{
			Workers: v.workers, Batch: v.batch, Skip: v.skip, Existing: existing,
			ProgressDone: base, ProgressTotal: total,
		})
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail-leaf: write: %v\n", err)
		return 1
	}

	res := map[string]any{
		"hive": hive, "db": dbpath, "written": written,
		"write_ms": time.Since(start).Milliseconds(),
	}
	if v.jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(res)
	} else {
		fmt.Printf("mail-leaf: committed hive=%s into %s: %d leafs written (%dms)\n",
			hive, dbpath, written, res["write_ms"])
	}
	return 0
}

func ensureWritable(dbpath, repoRoot string, cfg *config.Config, force bool) error {
	var reasons []string
	if holders, err := brain.LiveHolders(dbpath); err != nil {
		return fmt.Errorf("live-holder check: %w", err)
	} else if len(holders) > 0 {
		reasons = append(reasons, fmt.Sprintf("%s is open by %d process(es):\n  %s", dbpath, len(holders), strings.Join(holders, "\n  ")))
	}
	if repoRoot != "" && dbpath == filepath.Join(repoRoot, "var", "kb.lbug") &&
		brain.BrainAPIAlive("127.0.0.1:"+strconv.Itoa(cfg.Port)) {
		reasons = append(reasons, fmt.Sprintf("a brain API is answering on 127.0.0.1:%d (compose brain bind-mounts this db)", cfg.Port))
	}
	if len(reasons) > 0 && !force {
		return fmt.Errorf("refuse write; stop/restart the brain first (or pass --force):\n%s", strings.Join(reasons, "\n"))
	}
	return nil
}
