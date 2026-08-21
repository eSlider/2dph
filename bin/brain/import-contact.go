//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug
// bin/brain/import-contact.go - read address-book files and upsert each contact
// as an info-root leaf in the 2dph brain.
//
//	./bin/brain/import-contact.go --sources a.csv --db var/kb.lbug
//	./bin/brain/import-contact.go --sources dir/ --dry-run
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/contact"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		sources []string
		dbPath  string
		dryRun  bool
	)
	p := cli.New("brain-import-contact")
	p.Description = "upsert address-book contacts into the 2dph brain (info-root leafs)"
	p.StringSlice(&sources, "s", "sources", "comma-separated files or dirs to read")
	p.String(&dbPath, "", "db", "path to kb.lbug (default var/kb.lbug)")
	p.Bool(&dryRun, "", "dry-run", "print parsed counts only, write nothing")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "brain-import-contact: --sources is required")
		return 2
	}
	var srcs []string
	for _, s := range sources {
		for _, part := range strings.Split(s, ",") {
			if p := strings.TrimSpace(part); p != "" {
				srcs = append(srcs, p)
			}
		}
	}
	cs, err := contact.Load(srcs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-contact: %v\n", err)
		return 1
	}
	cs = contact.Dedupe(cs)
	contact.PrintCounts(cs)
	if dryRun {
		return 0
	}
	if dbPath == "" {
		dbPath = filepath.Join(brain.RepoRoot(), "var", "kb.lbug")
	}
	db, conn, err := brain.OpenWritable(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-contact: open brain: %v\n", err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-contact: schema: %v\n", err)
		return 1
	}
	leafs := make([]brain.LeafInput, 0, len(cs))
	for _, ct := range cs {
		leafs = append(leafs, brain.LeafInput{
			Text:   ct.Markdown(),
			Root:   "info",
			Type:   "contact",
			How:    "brain/import-contact",
			Source: ct.Source,
			Loc:    ct.Source,
		})
	}
	ids, err := brain.AddLeafs(conn, leafs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-contact: write: %v\n", err)
		return 1
	}
	if err := brain.EnsureIndexes(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain-import-contact: indexes: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "brain: wrote %d leaf(s) to %s\n", len(ids), dbPath)
	return 0
}
