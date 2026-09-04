//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug
//
// bin/brain/import-git.go - read git history (go-git, no git binary) and upsert
// commit leafs into the 2dph brain. Тонкая обёртка над git-адаптером корпуса
// (internal/corpus/git.go): Stream → WriteCorpus (id=ContentHash, P-9.3).
//
//	./bin/brain/import-git.go [REPO] --limit 100
//	./bin/brain/import-git.go --root ~/projects --dry-run
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/corpus"
	"github.com/eSlider/2dph/internal/gitlog"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/repo"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		repoArg string
		root    string
		since   string
		limit   int
		dbPath  string
		dryRun  bool
	)
	p := cli.New("brain-import-git")
	p.Description = "read git history (go-git) and upsert commit leafs into the brain"
	p.AddPositionalValue(&repoArg, "repo", 1, false, "git repo path")
	p.String(&root, "", "root", "scan dir for git repos")
	p.String(&since, "", "since", "RFC3339 or YYYY-MM-DD")
	p.Int(&limit, "", "limit", "max commits per repo (0 = all)")
	p.String(&dbPath, "", "db", "path to kb.lbug (default var/kb.lbug)")
	p.Bool(&dryRun, "", "dry-run", "print counts only, write nothing")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}

	sinceT, err := gitlog.ParseSince(since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: %v\n", err)
		return 2
	}

	var repos []string
	switch {
	case repoArg != "":
		repos = []string{repoArg}
	case root != "":
		// root сканируется адаптером (Git.Root); repos не задаём
	default:
		repos = []string{repo.Root()}
	}
	src := corpus.Git{Repos: repos, Root: root, Limit: limit, Since: sinceT}

	var leafs []contract.Leaf
	if err := src.Stream(context.Background(), func(l contract.Leaf) error {
		leafs = append(leafs, l)
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: %v\n", err)
		return 1
	}
	if dryRun {
		fmt.Fprintf(os.Stderr, "brain-import-git: %d commit leaf(s), dry-run\n", len(leafs))
		return 0
	}
	if dbPath == "" {
		dbPath = filepath.Join(brain.RepoRoot(), "var", "kb.lbug")
	}
	db, conn, err := brain.OpenWritable(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: open brain: %v\n", err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: schema: %v\n", err)
		return 1
	}
	n, err := brain.WriteCorpus(conn, leafs, nil, brain.WriteOptions{Workers: 4, Batch: 64})
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: write: %v\n", err)
		return 1
	}
	if err := brain.EnsureIndexes(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: indexes: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "brain: wrote %d leaf(s) to %s\n", n, dbPath)
	return 0
}
