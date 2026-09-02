//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,git_facts "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && git_facts
//
// bin/git/facts.go - deduction facts from git history in the Ladybug graph:
// «кто работал над <repo> когда и что сделал» (L-9.4 #233). Read-only.
// Reads Commit/Person/AUTHORED nodes (written by bin/git/graph.go) and emits
// deterministic per-(repo, person) audit cards (claim/premises/inference/
// gaps/verdict — внутренний gitgraph.GroupFacts/BuildFacts).
//
//	./bin/git/facts.go                              # all repos
//	./bin/git/facts.go --repo 2dph --since 2026-01-01
//	./bin/git/facts.go --author ada@example.com --commits --json
//	KB_ROOT=/path/to/2dph ...                       # db default <root>/var/kb.lbug
//
// Weakness (границы дедукции, docs/audit-recipes.md): коммиты с пустым или
// служебным subject («Update», WIP) дают только «трогал файлы» — карточка
// помечается weaken + OPEN, «что именно» не выводится. Merge-коммиты в граф
// не пишутся (gitlog.Log пропускает), co-author невидим — ограничение.
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/gitgraph"
	cliparse "github.com/eSlider/2dph/pkg/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

type factsFlags struct {
	repo, author, since, db string
	limit                   int
	jsonOut                 bool
	showCommits             bool
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

func run(args []string) int {
	var v factsFlags
	p := cliparse.New("git-facts")
	p.Description = "deduction facts from git history: кто работал над repo когда (Commit/Person/AUTHORED)"
	p.String(&v.repo, "", "repo", "filter by repo name (default all)")
	p.String(&v.author, "", "author", "filter by author email (default all)")
	p.String(&v.since, "", "since", "RFC3339 or YYYY-MM-DD (commit date >=)")
	p.String(&v.db, "", "db", "path to kb.lbug (default <root>/var/kb.lbug)")
	p.Int(&v.limit, "", "limit", "max fact cards (0 = all)")
	p.Bool(&v.showCommits, "", "commits", "list individual commits under each fact")
	p.Bool(&v.jsonOut, "", "json", "JSON output (audit cards)")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}

	sinceT, err := gitParseSince(v.since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-facts: %v\n", err)
		return 2
	}

	dbpath := v.db
	if dbpath == "" {
		root := os.Getenv("KB_ROOT")
		if root == "" {
			root = gitRepoRoot()
		}
		dbpath = filepath.Join(root, "var", "kb.lbug")
	}
	conn, closeConn, err := openReadOnly(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-facts: open %s (read-only): %v\n", dbpath, err)
		return 1
	}
	defer closeConn()

	rows, err := queryRows(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-facts: query: %v\n", err)
		return 1
	}
	facts := gitgraph.BuildFacts(gitgraph.GroupFacts(rows, gitgraph.FactFilter{
		Repo: v.repo, Author: v.author, Since: sinceT,
	}), v.showCommits)

	if v.limit > 0 && len(facts) > v.limit {
		facts = facts[:v.limit]
	}
	if v.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(facts); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if len(facts) == 0 {
		fmt.Println("git-facts: no facts (graph empty or filters miss)")
		return 0
	}
	for _, f := range facts {
		fmt.Printf("repo=%s person=%s\n", f.Repo, f.Person)
		fmt.Printf("  claim: %s\n", f.Claim)
		for _, g := range f.Gaps {
			fmt.Printf("  gap: %s\n", g)
		}
		fmt.Printf("  inference: %s; verdict: %s\n", f.Inference, f.Verdict)
		if v.showCommits {
			for _, c := range f.Rows {
				fmt.Printf("    %s %s %s\n", shortID(c.ID), c.Date, c.Subject)
			}
		}
	}
	return 0
}

// openReadOnly открывает kb.lbug с ReadOnly=true — работает параллельно с
// живым RW-сервисом (Ladybug: второй RW-open невозможен, RO-open разрешён).
func openReadOnly(dbpath string) (*lbug.Connection, func(), error) {
	cfg := lbug.DefaultSystemConfig()
	cfg.ReadOnly = true
	cfg.MaxNumThreads = 8
	db, err := lbug.OpenDatabase(dbpath, cfg)
	if err != nil {
		return nil, nil, err
	}
	conn, err := lbug.OpenConnection(db)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return conn, func() {
		conn.Close()
		db.Close()
	}, nil
}

func gitParseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse --since %q", s)
}

// queryRows читает все Commit-AUTHORED-Person тройки.
func queryRows(conn *lbug.Connection) ([]gitgraph.FactRow, error) {
	res, err := conn.Query(`MATCH (c:Commit)-[:AUTHORED]->(p:Person)
		RETURN c.id, c.repo, c.subject, c.date, p.id, p.name`)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	var out []gitgraph.FactRow
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 6 {
			return nil, fmt.Errorf("commit row: %v %v", vals, err)
		}
		out = append(out, gitgraph.FactRow{
			ID:      fmt.Sprint(vals[0]),
			Repo:    fmt.Sprint(vals[1]),
			Subject: fmt.Sprint(vals[2]),
			Date:    fmt.Sprint(vals[3]),
			Email:   fmt.Sprint(vals[4]),
			Name:    fmt.Sprint(vals[5]),
		})
	}
	return out, nil
}

func shortID(id string) string {
	i := strings.LastIndexByte(id, ':')
	if i >= 0 && len(id)-i-1 > 12 {
		return id[:i+1] + id[i+1:i+13]
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
