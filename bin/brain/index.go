//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_index "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_index
//
// bin/brain/index.go - rebuild the Ladybug graph (Go+Zig write path).
//
//	./bin/brain/index.go --rebuild --with-facts --with-chats
//	./bin/brain/index.go --rebuild --with-mail
//	./bin/brain/index.go --rebuild --git-root ~/projects
//	./bin/brain/index.go --dry-run --with-mail
//
// P-9.3: index — драйвер адаптеров корпуса (internal/corpus). Каждый корпус
// (docs/mail/chats/git) реализует contract.Source; флаги включают/выключают
// адаптеры, единый writer (brain.WriteCorpus) пишет leafs с id=ContentHash.
// facts остаются отдельным путём (не корпус, а evidence gate).
//
// Bulk mail/corpus still --rebuild (fresh file, indexes last).
// NOTE: never run gofmt -w on this file — it breaks the shebang.
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

type indexFlags struct {
	db, factsJSON, withChats, since, gitRoot                                            string
	corpus                                                                              []string
	rebuild, noDefaults, withMail, withFacts, dryRun, skipIndexes, jsonOut, skip, force bool
	limit, workers, batch, progress                                                     int
}

// gitRepoRoot resolves the actual repository checkout (independent of
// KB_ROOT, which points the corpus/db at arbitrary roots for tests and
// throwaway builds).
func gitRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// sources собирает адаптеры корпуса по флагам (P-9.3): добавление/удаление
// корпуса = добавление/удаление адаптера здесь + пересборка.
func sources(v indexFlags, root string) []contract.Source {
	ss := []contract.Source{
		corpus.Docs{Root: root, ExtraDirs: v.corpus, NoDefaults: v.noDefaults},
	}
	if v.withMail {
		ss = append(ss, corpus.Mail{Root: root, Since: v.since})
	}
	if v.withChats != "" {
		ss = append(ss, corpus.Chats{Root: root, Dir: v.withChats})
	}
	if v.gitRoot != "" {
		ss = append(ss, corpus.Git{Root: v.gitRoot})
	}
	return ss
}

func run(args []string) int {
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: config: %v\n", err)
		return 1
	}
	brain.Configure(cfg)

	// historic wrapper prepended --with-mail; keep default off unless passed
	v := indexFlags{}
	p := cliparse.New("brain-index")
	p.Description = "build the 2dph brain index (Go+Zig)"
	p.String(&v.db, "", "db", "path to kb.lbug")
	p.Bool(&v.rebuild, "", "rebuild", "fresh db + indexes")
	p.Bool(&v.noDefaults, "", "no-defaults", "skip README/docs/skills")
	p.Bool(&v.withMail, "", "with-mail", "include var/corpus/mail + legacy var/mail message.md leafs")
	p.Bool(&v.withFacts, "", "with-facts", "facts/extract --json --dry-run")
	p.String(&v.factsJSON, "", "facts-json", "JSON facts file")
	p.String(&v.withChats, "", "with-chats", "chat markdown dir (empty=off; bare flag via const path)")
	p.String(&v.gitRoot, "", "git-root", "dir of git repos to import into the brain")
	p.String(&v.since, "", "since", "with --with-mail, messages >= YYYY-MM-DD")
	p.Bool(&v.dryRun, "", "dry-run", "count leafs, write nothing")
	p.Bool(&v.skipIndexes, "", "skip-indexes", "write leafs only")
	p.Int(&v.limit, "", "limit", "max leafs to embed")
	p.Int(&v.workers, "", "workers", "parallel embedding workers (default 4)")
	p.Int(&v.batch, "", "batch", "leafs per transaction (default 64)")
	p.Int(&v.progress, "", "progress", "progress/ETA line every N seconds")
	p.Bool(&v.skip, "", "skip", "skip leafs already in the db (resume)")
	p.Bool(&v.force, "", "force", "rebuild even if the db is open by a live process")
	p.Bool(&v.jsonOut, "", "json", "JSON stats")
	// --corpus may repeat: parse manually from args leftovers after flaggy
	if err := cliparse.Parse(p, filterCorpusArgs(args, &v.corpus)); err != nil {
		return cliparse.Fail(err)
	}
	// bare --with-chats without value → default dir (var/corpus/chats/md)
	if hasBareWithChats(args) && v.withChats == "" {
		v.withChats = filepath.Join(brain.RepoRoot(), "var", "corpus", "chats", "md")
	}

	root := brain.RepoRoot()
	dbpath := v.db
	if dbpath == "" {
		dbpath = filepath.Join(root, "var", "kb.lbug")
	}
	port := strconv.Itoa(cfg.Port)
	if cfg.Port <= 0 {
		port = "8630"
	}

	// Единый проход: адаптеры корпуса → collect → WriteCorpus (P-9.3).
	var leafs []contract.Leaf
	perSource := map[string]int{}
	for _, s := range sources(v, root) {
		before := len(leafs)
		if err := s.Stream(context.Background(), func(l contract.Leaf) error {
			leafs = append(leafs, l)
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: corpus %s: %v\n", s.Name(), err)
			return 1
		}
		perSource[s.Name()] = len(leafs) - before
	}

	facts := []brain.LeafInput{}
	if v.factsJSON != "" {
		raw, err := os.ReadFile(v.factsJSON)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: facts-json: %v\n", err)
			return 1
		}
		facts = append(facts, factsFromJSON(raw)...)
	}
	if v.withFacts {
		facts = append(facts, factsFromExtract(root)...)
	}

	if v.dryRun {
		msg := map[string]any{
			"info": len(leafs), "facts": len(facts), "by_source": perSource,
			"would_index": true,
		}
		if v.jsonOut {
			enc := json.NewEncoder(os.Stdout)
			_ = enc.Encode(msg)
		} else {
			fmt.Printf("brain/index: %d info + %d facts would be indexed (%v)\n", len(leafs), len(facts), perSource)
		}
		return 0
	}
	if !v.rebuild && !v.skip {
		fmt.Fprintln(os.Stderr, "brain/index: refuse write; pass --rebuild (fresh db) or --skip (resume existing db)")
		return 2
	}

	if !v.skip {
		// A live holder of this file (e.g. brain-serve in a compose container
		// bind-mounting the same var/) would keep serving the removed inode:
		// reads go stale and the fresh db lands with the writer's uid, locking
		// out the service. Refuse unless --force. Two probes: same-namespace
		// fd holders via /proc, and (for the default repo db) any brain API
		// answering on 127.0.0.1:$KB_PORT — container fds are invisible to
		// host /proc when the service runs as another uid.
		var reasons []string
		if holders, err := brain.LiveHolders(dbpath); err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: live-holder check: %v\n", err)
			return 1
		} else if len(holders) > 0 {
			reasons = append(reasons, fmt.Sprintf("%s is open by %d process(es):\n  %s", dbpath, len(holders), strings.Join(holders, "\n  ")))
		}
		if gitRoot := gitRepoRoot(); gitRoot != "" &&
			dbpath == filepath.Join(gitRoot, "var", "kb.lbug") &&
			!cfg.IndexAllowLive &&
			brain.BrainAPIAlive("127.0.0.1:"+port) {
			reasons = append(reasons, fmt.Sprintf("a brain API is answering on 127.0.0.1:%s (compose brain bind-mounts this db)", port))
		}
		if len(reasons) > 0 && !v.force {
			fmt.Fprintf(os.Stderr, "brain/index: refuse --rebuild; stop/restart the brain first (or pass --force):\n%s\n", strings.Join(reasons, "\n"))
			return 2
		}
		_ = os.Remove(dbpath)
		_ = os.Remove(dbpath + ".wal")
	}

	model, err := brain.LoadModel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: model: %v\n", err)
		return 1
	}
	defer model.Close()

	db, conn, err := brain.OpenWritable(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: %v\n", err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: schema: %v\n", err)
		return 1
	}

	prog := brain.NewProgressReporter(os.Stderr, time.Duration(v.progress)*time.Second)
	opt := brain.WriteOptions{
		Limit: v.limit, Workers: v.workers, Batch: v.batch, Skip: v.skip,
		Progress: prog,
	}
	infoN, err := brain.WriteCorpus(conn, leafs, model, opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: write info: %v\n", err)
		return 1
	}
	factN := 0
	for _, f := range facts {
		if f.Text == "" || f.Source == "" {
			continue
		}
		if len(f.Embedding) == 0 && model != nil {
			vec, err := model.Embed(f.Text)
			if err != nil {
				fmt.Fprintf(os.Stderr, "brain/index: embed fact: %v\n", err)
				return 1
			}
			f.Embedding = vec
		}
		if f.Root == "" {
			f.Root = "facts"
		}
		if f.Type == "" {
			f.Type = "fact"
		}
		if f.How == "" {
			f.How = "facts/extract"
		}
		if _, err := brain.UpsertLeaf(conn, f); err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: fact: %v\n", err)
			return 1
		}
		factN++
	}
	if !v.skipIndexes {
		if err := brain.EnsureIndexes(conn); err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: indexes: %v\n", err)
			return 1
		}
	}
	total := infoN + factN
	if v.jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"indexed_info": infoN, "indexed_facts": factN, "db": dbpath, "total": total,
			"by_source": perSource,
		})
	} else {
		fmt.Printf("indexed %d/%d info + %d facts; db total %d\n", infoN, len(leafs), factN, total)
	}
	return 0
}

func filterCorpusArgs(args []string, corpus *[]string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--corpus" && i+1 < len(args):
			*corpus = append(*corpus, args[i+1])
			i++
		case strings.HasPrefix(a, "--corpus="):
			*corpus = append(*corpus, strings.TrimPrefix(a, "--corpus="))
		default:
			out = append(out, a)
		}
	}
	return out
}

func hasBareWithChats(args []string) bool {
	for i, a := range args {
		if a == "--with-chats" {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return true
			}
		}
	}
	return false
}

func factsFromExtract(root string) []brain.LeafInput {
	cmd := exec.Command(filepath.Join(root, "bin", "cgo", "zig"), "go", "run",
		"-tags=system_ladybug,facts_extract",
		filepath.Join(root, "bin", "facts", "extract.go"), "--json", "--dry-run")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/index: facts/extract failed: %v\n", err)
		return nil
	}
	return factsFromJSON(out)
}

func factsFromJSON(raw []byte) []brain.LeafInput {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		return nil
	}
	var list []any
	switch t := payload.(type) {
	case map[string]any:
		list, _ = t["facts"].([]any)
	case []any:
		list = t
	}
	var out []brain.LeafInput
	for _, x := range list {
		m, ok := x.(map[string]any)
		if !ok {
			continue
		}
		text := fmt.Sprint(m["text"])
		source := fmt.Sprint(m["source"])
		if text == "" || source == "" || text == "<nil>" || source == "<nil>" {
			continue
		}
		out = append(out, brain.LeafInput{
			Text: text, Source: source, Root: "facts", Confidence: "confirmed",
			SourceRev:  strOr(m["source_rev"], "working-tree"),
			How:        strOr(m["how"], "facts/extract"),
			Loc:        strOr(m["loc"], source),
			Type:       "fact",
			ExternalID: strOr(m["external_id"], ""),
			ObservedAt: strOr(m["observed_at"], ""),
		})
	}
	return out
}

func strOr(v any, def string) string {
	if v == nil {
		return def
	}
	s := fmt.Sprint(v)
	if s == "" || s == "<nil>" {
		return def
	}
	return s
}
