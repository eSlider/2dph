//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,facts_promote "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && facts_promote
//
// bin/facts/promote.go - promote confirmed info leafs with >=2 sources to facts-root.
//
//	./bin/facts/promote.go [--db PATH] [--dry-run] [--json]
//
// Evidence-first: a leaf only becomes a fact when its source string carries >=2
// independent sources (facts/extract convention "a x b"). Single-source leafs
// stay in info. Idempotent: promoted leafs are no longer root=info, so a re-run
// is a no-op. Production #181: the facts-root was empty because var/kb.lbug was
// rebuilt without --with-facts; this tool repairs the facts layer and
// bin/facts/extract.go refills it with 2-source facts.
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/config"
	cliparse "github.com/eSlider/2dph/pkg/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/promote: config: %v\n", err)
		return 1
	}
	brain.Configure(cfg)

	var dbpath string
	var jsonOut, dryRun bool
	p := cliparse.New("facts-promote")
	p.Description = "promote confirmed info leafs with >=2 sources to facts-root"
	p.String(&dbpath, "", "db", "path to kb.lbug (default var/kb.lbug)")
	p.Bool(&jsonOut, "", "json", "JSON output")
	p.Bool(&dryRun, "", "dry-run", "print candidates, write nothing")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}

	if dbpath == "" {
		dbpath = filepath.Join(brain.RepoRoot(), "var", "kb.lbug")
	}

	db, conn, err := brain.OpenWritable(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/promote: %v\n", err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "facts/promote: schema: %v\n", err)
		return 1
	}

	byRoot, err := rootCounts(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/promote: %v\n", err)
		return 1
	}
	before := map[string]int{}
	for k, v := range byRoot {
		before[k] = v
	}

	candidates, err := brain.EligibleInfoLeafs(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/promote: %v\n", err)
		return 1
	}

	promoted := 0
	if !dryRun && len(candidates) > 0 {
		if _, err := brain.AddLeafs(conn, candidates); err != nil {
			fmt.Fprintf(os.Stderr, "facts/promote: %v\n", err)
			return 1
		}
		promoted = len(candidates)
	} else if dryRun {
		promoted = len(candidates) // reported, not written
	}

	after := map[string]int{}
	if dryRun {
		// nothing written: after == before
		for k, v := range before {
			after[k] = v
		}
	} else {
		ar, err := rootCounts(conn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "facts/promote: %v\n", err)
			return 1
		}
		for k, v := range ar {
			after[k] = v
		}
	}

	report := map[string]any{
		"dry_run":   dryRun,
		"promoted":  promoted,
		"before":    before,
		"after":     after,
		"candidates": candidatesOut(candidates),
		"db":        dbpath,
	}
	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(report)
		return 0
	}
	fmt.Printf("facts/promote: %d candidates with >=2 sources (dry_run=%v)\n", promoted, dryRun)
	fmt.Printf("  before: facts=%d info=%d\n", before["facts"], before["info"])
	fmt.Printf("  after:  facts=%d info=%d\n", after["facts"], after["info"])
	for _, c := range candidates {
		fmt.Printf("  -> %s  [%s]\n", c.Text, c.Source)
	}
	return 0
}

func rootCounts(conn *lbug.Connection) (map[string]int, error) {
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, count(*)")
	if err != nil {
		return nil, err
	}
	defer res.Close()
	out := map[string]int{}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			continue
		}
		out[fmt.Sprint(vals[0])] = int(asInt(vals[1]))
	}
	return out, nil
}

func asInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func candidatesOut(leafs []brain.LeafInput) []map[string]string {
	out := make([]map[string]string, 0, len(leafs))
	for _, lf := range leafs {
		out = append(out, map[string]string{"text": lf.Text, "source": lf.Source})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["source"] < out[j]["source"] })
	return out
}
