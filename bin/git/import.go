//usr/bin/env go run "$0" "$@"; exit
//
// bin/git/import.go - read git history with go-git (no git binary).
//
//	./bin/git/import.go [REPO]
//	./bin/git/import.go --json
//	./bin/git/import.go --limit 100 --since 2026-01-01
//	./bin/git/import.go --root DIR
//
// Conversion only: prints commit leafs. Brain write is bin/brain/index.go.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	cliparse "github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/cmdbin"
	"github.com/eSlider/2dph/internal/gitlog"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	c, err := gitlog.ParseArgs(args)
	if err != nil {
		return cliparse.Fail(err)
	}
	repo, root, since, limit, jsonOut := c.Repo, c.Root, c.Since, c.Limit, c.JSONOut

	sinceT, err := gitlog.ParseSince(since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "git/import: %v\n", err)
		return 2
	}

	repos := []string{}
	if repo != "" {
		repos = []string{repo}
	} else if root != "" {
		entries, err := os.ReadDir(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git/import: %v\n", err)
			return 1
		}
		for _, e := range entries {
			p := filepath.Join(root, e.Name())
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				repos = append(repos, p)
			}
		}
	} else {
		repos = []string{cmdbin.Root()}
	}

	opt := gitlog.Options{Limit: limit, Since: sinceT}
	type row struct {
		Repo    string         `json:"repo"`
		Path    string         `json:"path"`
		Commits int            `json:"commits"`
		Leafs   []gitlog.Leaf  `json:"leafs,omitempty"`
	}
	var rows []row
	for _, p := range repos {
		name, err := gitlog.RepoName(p)
		if err != nil && name == "" {
			fmt.Fprintf(os.Stderr, "git/import: %s: %v\n", p, err)
			continue
		}
		cs, err := gitlog.Log(p, opt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git/import: %s: %v\n", p, err)
			return 1
		}
		leafs := make([]gitlog.Leaf, 0, len(cs))
		for _, c := range cs {
			leafs = append(leafs, gitlog.ToLeaf(c, name))
		}
		rows = append(rows, row{Repo: name, Path: p, Commits: len(cs), Leafs: leafs})
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(rows); err != nil {
			return 1
		}
		return 0
	}
	for _, r := range rows {
		fmt.Printf("%-24s %5d commits  %s\n", r.Repo, r.Commits, r.Path)
	}
	return 0
}
