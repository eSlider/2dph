//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,facts_audit_db "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && facts_audit_db
//
// bin/facts/audit-db.go - evidence gate over var/kb.lbug (root=facts).
//
//	./bin/facts/audit-db.go [--json]
//
// Go implementation of the former python `bin/facts/audit db`. Loads every
// Leaf with root=facts and runs the two-source checks (confirmed needs
// ` x `; hypothesis contradictions need `a x b vs c x d`).
// Exit 0 = all checks pass, 1 = audit failures, 2 = could not evaluate.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/facts"
)

func main() {
	path := filepath.Join(brain.RepoRoot(), "var", "kb.lbug")
	if _, err := os.Stat(path); err != nil {
		fmt.Println(`{"mode":"db","ok":false,"problems":["no database yet; run bin/brain/index.go first"]}`)
		os.Exit(1)
	}
	db, conn, err := brain.OpenWritable(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit db: open:", err)
		os.Exit(2)
	}
	defer db.Close()
	defer conn.Close()

	res, err := conn.Query("MATCH (l:Leaf {root:'facts'}) RETURN l.id, l.source, l.loc, l.how, l.confidence")
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit db: query:", err)
		os.Exit(2)
	}
	var problems []string
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			continue
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 5 {
			continue
		}
		problems = append(problems, facts.CheckFactRow(
			fmt.Sprint(vals[0]), fmt.Sprint(vals[1]),
			fmt.Sprint(vals[2]), fmt.Sprint(vals[3]), fmt.Sprint(vals[4]),
		)...)
	}

	out := map[string]any{"mode": "db", "ok": len(problems) == 0, "problems": problems}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit db: marshal:", err)
		os.Exit(2)
	}
	fmt.Println(string(b))
	if len(problems) > 0 {
		os.Exit(1)
	}
}