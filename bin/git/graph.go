//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,git_graph "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && git_graph
//
// bin/git/graph.go - import git history into the Ladybug graph as Commit/Person
// nodes + AUTHORED edges (L-9.4 #233).
//
//	./bin/git/graph.go --root /x/git-repos --dry-run          # read+map+count, write nothing
//	./bin/git/graph.go --repo /x/2dph,/x/gator --dry-run
//	./bin/git/graph.go --root /x/git-repos --commit            # MERGE into <root>/var/kb.lbug
//	./bin/git/graph.go --repo /x/2dph --commit --db /x/kb.lbug --skip-existing --since 2026-01-01
//
// Reads git history with go-git (internal/gitlog — the same source as the git
// corpus Leaf premises, kind=commit; merges skipped). Commit id canon =
// <repo>:<sha> (repo = origin-remote name), Person.id = email lowercase (the
// same canon as the mail graph D-1 #257 — same email merges into one Person).
// Dry-run by default; --commit writes idempotently (MERGE) — a repeated run
// creates zero duplicates. Live-holder guard: refuses to write while the db
// file is open by a live process / brain API unless --force.
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

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/gitgraph"
	"github.com/eSlider/2dph/internal/gitlog"
	cliparse "github.com/eSlider/2dph/pkg/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

type graphFlags struct {
	repos        []string
	root, db     string
	since        string
	limit        int
	dryRun       bool
	commit       bool
	skipExisting bool
	force        bool
	jsonOut      bool
	batch        int
}

// gitRepoRoot resolves the actual repository checkout (KB_ROOT may point the
// db at arbitrary roots for tests).
func gitRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// countQuery исполняет Cypher вида MATCH ... RETURN count(...) и возвращает
// первое скалярное значение.
func countQuery(conn *lbug.Connection, q string) (int, error) {
	res, err := conn.Query(q)
	if err != nil {
		return 0, err
	}
	defer res.Close()
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return 0, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			return 0, fmt.Errorf("count row: %v", err)
		}
		n, err := strconv.Atoi(fmt.Sprint(vals[0]))
		if err != nil {
			return 0, fmt.Errorf("count value %v: %w", vals[0], err)
		}
		return n, nil
	}
	return 0, nil
}

// existingCommitIDs собирает id уже импортированных Commit-узлов
// (инкрементальный sync --skip-existing).
func existingCommitIDs(conn *lbug.Connection) (map[string]bool, error) {
	res, err := conn.Query("MATCH (c:Commit) RETURN c.id")
	if err != nil {
		return nil, err
	}
	defer res.Close()
	out := map[string]bool{}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			return nil, fmt.Errorf("commit id row: %v", err)
		}
		out[fmt.Sprint(vals[0])] = true
	}
	return out, nil
}

func run(args []string) int {
	var v graphFlags
	p := cliparse.New("git-graph")
	p.Description = "import git history into the Ladybug graph (Commit/Person + AUTHORED)"
	p.StringSlice(&v.repos, "", "repo", "comma-separated git repo paths (default: --root scan)")
	p.String(&v.root, "", "root", "dir scanned for git repos (default repo dir)")
	p.String(&v.since, "", "since", "RFC3339 or YYYY-MM-DD (commits after)")
	p.Int(&v.limit, "", "limit", "max commits per repo (0 = all)")
	p.String(&v.db, "", "db", "path to kb.lbug (default <root>/var/kb.lbug)")
	p.Bool(&v.dryRun, "", "dry-run", "read+map+count only, write nothing")
	p.Bool(&v.commit, "", "commit", "write Commit/Person nodes + AUTHORED edges into kb.lbug")
	p.Bool(&v.skipExisting, "", "skip-existing", "incremental: skip commit ids already in the graph")
	p.Bool(&v.force, "", "force", "write even if the db is open by a live process")
	p.Bool(&v.jsonOut, "", "json", "JSON result")
	p.Int(&v.batch, "", "batch", "commits per transaction (default 500)")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	if v.commit && v.dryRun {
		fmt.Fprintln(os.Stderr, "git-graph: --dry-run and --commit are mutually exclusive")
		return 2
	}
	if len(v.repos) == 0 && v.root == "" {
		fmt.Fprintln(os.Stderr, "git-graph: pass --repo <path> or --root <dir>")
		return 2
	}

	sinceT, err := gitlog.ParseSince(v.since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-graph: %v\n", err)
		return 2
	}

	// Read + map (dry-run default).
	ctx := context.Background()
	start := time.Now()
	repos, err := gitgraph.Resolve(v.root, splitCSV(v.repos))
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-graph: resolve repos: %v\n", err)
		return 1
	}
	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "git-graph: no git repos found (scan --root or pass --repo)")
		return 1
	}
	rcs, err := gitgraph.ReadAll(ctx, repos, gitlog.Options{Limit: v.limit, Since: sinceT})
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-graph: read git history: %v\n", err)
		return 1
	}
	inputs := gitgraph.ToInputs(rcs)
	st := gitgraph.ComputeStats(inputs)
	readMs := time.Since(start).Milliseconds()

	res := map[string]any{
		"repos":   repoNames(repos),
		"read_ms": readMs,
		"total":   st.Total,
		"byRepo":  st.Repos,
		"emails":  st.Emails,
		"weak":    st.Weak,
		"inputs":  len(inputs),
	}

	if !v.commit { // dry-run по умолчанию
		if v.jsonOut {
			enc := json.NewEncoder(os.Stdout)
			_ = enc.Encode(res)
		} else {
			fmt.Printf("git-graph: dry-run repos=%s: %d commits (%d inputs), %d unique author emails, %d weak subjects\n",
				strings.Join(repoNames(repos), ","), st.Total, len(inputs), st.Emails, st.Weak)
			for _, rc := range st.Repos {
				fmt.Printf("  repo %-20s %d\n", rc.Repo, rc.N)
			}
			fmt.Printf("  read %dms\n", readMs)
		}
		return 0
	}

	// Commit: запись в kb.lbug (MERGE, идемпотентно).
	cfg, err := config.Load(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-graph: config: %v\n", err)
		return 1
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
		fmt.Fprintf(os.Stderr, "git-graph: %v\n", err)
		return 2
	}

	db, conn, err := brain.OpenWritable(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-graph: open %s: %v\n", dbpath, err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "git-graph: schema: %v\n", err)
		return 1
	}

	before := map[string]int{}
	for _, label := range []string{"Commit", "Person"} {
		if before[label], err = countQuery(conn, "MATCH (n:"+label+") RETURN count(n)"); err != nil {
			fmt.Fprintf(os.Stderr, "git-graph: count: %v\n", err)
			return 1
		}
	}
	before["AUTHORED"], err = countQuery(conn, "MATCH ()-[:AUTHORED]->() RETURN count(*)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-graph: count: %v\n", err)
		return 1
	}

	if v.skipExisting {
		have, err := existingCommitIDs(conn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git-graph: existing ids: %v\n", err)
			return 1
		}
		kept := inputs[:0]
		for _, in := range inputs {
			if !have[in.ID] {
				kept = append(kept, in)
			}
		}
		res["skippedExisting"] = len(inputs) - len(kept)
		inputs = kept
	}

	batch := v.batch
	if batch <= 0 {
		batch = 500
	}
	writeStart := time.Now()
	for i := 0; i < len(inputs); i += batch {
		end := i + batch
		if end > len(inputs) {
			end = len(inputs)
		}
		if err := brain.UpsertCommits(conn, inputs[i:end]); err != nil {
			fmt.Fprintf(os.Stderr, "git-graph: upsert batch %d..%d: %v\n", i, end, err)
			return 1
		}
	}
	writeMs := time.Since(writeStart).Milliseconds()

	after := map[string]int{}
	for _, label := range []string{"Commit", "Person"} {
		if after[label], err = countQuery(conn, "MATCH (n:"+label+") RETURN count(n)"); err != nil {
			fmt.Fprintf(os.Stderr, "git-graph: count: %v\n", err)
			return 1
		}
	}
	after["AUTHORED"], err = countQuery(conn, "MATCH ()-[:AUTHORED]->() RETURN count(*)")
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-graph: count: %v\n", err)
		return 1
	}
	res["db"] = dbpath
	res["write_ms"] = writeMs
	res["before"] = before
	res["after"] = after

	if v.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(res)
	} else {
		fmt.Printf("git-graph: committed repos=%s into %s: %d commits written (%dms)\n",
			strings.Join(repoNames(repos), ","), dbpath, len(inputs), writeMs)
		fmt.Printf("  Commit %d → %d, Person %d → %d, AUTHORED %d → %d\n",
			before["Commit"], after["Commit"], before["Person"], after["Person"],
			before["AUTHORED"], after["AUTHORED"])
	}
	return 0
}

func repoNames(repos []gitgraph.Repo) []string {
	out := make([]string, 0, len(repos))
	for _, r := range repos {
		out = append(out, r.Name)
	}
	return out
}

// splitCSV разворачивает comma-separated значения флага (StringSlice не
// режет по запятой — каждый элемент флага остаётся одним значением).
func splitCSV(values []string) []string {
	var out []string
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// ensureWritable — live-holder guard (как mail/graph.go): refuse запись,
// пока kb.lbug открыт живым процессом / brain API отвечает, без --force.
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
