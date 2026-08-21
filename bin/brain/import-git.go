//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug
//
// bin/brain/import-git.go - read git history (go-git, no git binary) and upsert
// commit leafs into the 2dph brain.
//
//	./bin/brain/import-git.go [REPO] --limit 100
//	./bin/brain/import-git.go --root ~/projects --dry-run
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/gitlog"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/repo"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		dbPath string
		dryRun bool
	)
	c, err := gitlog.ParseArgs(args)
	if err != nil {
		return cli.Fail(err)
	}
	p := cli.New("brain-import-git")
	p.String(&dbPath, "", "db", "path to kb.lbug (default var/kb.lbug)")
	p.Bool(&dryRun, "", "dry-run", "print counts only, write nothing")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}

	sinceT, err := gitlog.ParseSince(c.Since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: %v\n", err)
		return 2
	}

	repos := []string{}
	switch {
	case c.Repo != "":
		repos = []string{c.Repo}
	case c.Root != "":
		entries, err := os.ReadDir(c.Root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain-import-git: %v\n", err)
			return 1
		}
		for _, e := range entries {
			dir := filepath.Join(c.Root, e.Name())
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				repos = append(repos, dir)
			}
		}
	default:
		repos = []string{repo.Root()}
	}

	opt := gitlog.Options{Limit: c.Limit, Since: sinceT}
	var (
		leafs []brain.LeafInput
		total int
	)
	for _, rp := range repos {
		name, err := gitlog.RepoName(rp)
		if err != nil && name == "" {
			fmt.Fprintf(os.Stderr, "brain-import-git: %s: %v\n", rp, err)
			continue
		}
		cs, err := gitlog.Log(rp, opt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain-import-git: %s: %v\n", rp, err)
			return 1
		}
		for _, cm := range cs {
			l := gitlog.ToLeaf(cm, name)
			leafs = append(leafs, brain.LeafInput{
				Text:   l.Heading + "\n" + l.Text,
				Root:   "info",
				Type:   l.Type,
				How:    "git-log",
				Source: l.Source,
				Loc:    l.Repo,
			})
		}
		fmt.Fprintf(os.Stderr, "brain-import-git: %-24s %5d commits  %s\n", name, len(cs), rp)
		total += len(cs)
	}
	if dryRun {
		fmt.Fprintf(os.Stderr, "brain-import-git: %d commit leaf(s), dry-run\n", total)
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
	ids, err := brain.AddLeafs(conn, leafs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: write: %v\n", err)
		return 1
	}
	if err := brain.EnsureIndexes(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-git: indexes: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "brain: wrote %d leaf(s) to %s\n", len(ids), dbPath)
	return 0
}
