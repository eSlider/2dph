//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_add "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_add
//
// bin/brain/add.go - incremental leaf write (Go+Zig, no rebuild).
//
//	./bin/brain/add.go --text T --root facts --source "a.md x b.md"
//	./bin/brain/add.go --json
//
// Does not delete var/kb.lbug. NOTE: never run gofmt -w (breaks shebang).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	cliparse "github.com/eSlider/2dph/internal/cli"
	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

type addFlags struct {
	db, text, root, source, confidence, sourceRev, how, loc, typ string
	validFrom, validTo                                           string
	jsonIn                                                       bool
}

func run(args []string) int {
	v := addFlags{root: "info", confidence: "confirmed", sourceRev: "working-tree", how: "brain/add", typ: "reference"}
	p := cliparse.New("brain-add")
	p.Description = "add leafs without rebuilding the brain"
	p.String(&v.db, "", "db", "path to kb.lbug")
	p.Bool(&v.jsonIn, "", "json", "read leaf JSON from stdin")
	p.String(&v.text, "", "text", "leaf text")
	p.String(&v.root, "", "root", "facts|info")
	p.String(&v.source, "", "source", "evidence sources")
	p.String(&v.confidence, "", "confidence", "confirmed|partial|hypothesis")
	p.String(&v.sourceRev, "", "source-rev", "source revision")
	p.String(&v.how, "", "how", "how written")
	p.String(&v.loc, "", "loc", "location pointer")
	p.String(&v.typ, "", "type", "leaf type")
	p.String(&v.validFrom, "", "valid-from", "YYYY-MM-DD")
	p.String(&v.validTo, "", "valid-to", "YYYY-MM-DD inclusive")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	var leafs []brain.LeafInput
	if v.jsonIn {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return cliparse.Fail(err)
		}
		leafs, err = parseLeafJSON(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/add: %v\n", err)
			return 2
		}
	} else {
		if v.text == "" || v.source == "" {
			fmt.Fprintln(os.Stderr, "brain/add: --text and --source are required (or --json)")
			return 2
		}
		leafs = []brain.LeafInput{{
			Text: v.text, Root: v.root, Source: v.source, Confidence: v.confidence,
			SourceRev: v.sourceRev, How: v.how, Loc: or(v.loc, v.source), Type: v.typ,
			ValidFrom: v.validFrom, ValidTo: v.validTo,
		}}
	}
	for _, lf := range leafs {
		if lf.Text == "" || lf.Source == "" {
			fmt.Fprintln(os.Stderr, "brain/add: each leaf needs text and source")
			return 2
		}
	}
	model, err := brain.LoadModel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/add: model: %v\n", err)
		return 1
	}
	defer model.Close()
	for i := range leafs {
		if len(leafs[i].Embedding) > 0 {
			continue
		}
		vec, err := model.Embed(leafs[i].Text)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/add: embed: %v\n", err)
			return 1
		}
		leafs[i].Embedding = vec
	}
	dbpath := v.db
	if dbpath == "" {
		dbpath = filepath.Join(brain.RepoRoot(), "var", "kb.lbug")
	}
	db, conn, err := brain.OpenWritable(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/add: %v\n", err)
		return 1
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain/add: schema: %v\n", err)
		return 1
	}
	ids, err := brain.AddLeafs(conn, leafs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/add: write: %v\n", err)
		return 1
	}
	if err := brain.EnsureIndexes(conn); err != nil {
		fmt.Fprintf(os.Stderr, "brain/add: indexes: %v\n", err)
		return 1
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(map[string]any{"mode": "add", "ids": ids, "db": dbpath})
	return 0
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func parseLeafJSON(raw []byte) ([]brain.LeafInput, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var objs []map[string]any
	switch t := payload.(type) {
	case []any:
		for _, x := range t {
			m, ok := x.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("list items must be objects")
			}
			objs = append(objs, m)
		}
	case map[string]any:
		if leafs, ok := t["leafs"].([]any); ok {
			for _, x := range leafs {
				m, ok := x.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("leafs items must be objects")
				}
				objs = append(objs, m)
			}
		} else {
			objs = []map[string]any{t}
		}
	default:
		return nil, fmt.Errorf("json must be an object, a list, or {leafs:[...]}")
	}
	out := make([]brain.LeafInput, 0, len(objs))
	for _, m := range objs {
		lf := brain.LeafInput{
			Text: str(m["text"]), Root: str(m["root"]), Source: str(m["source"]),
			Confidence: str(m["confidence"]), SourceRev: str(m["source_rev"]),
			How: str(m["how"]), Loc: str(m["loc"]), Type: firstStr(m, "type", "type_"),
			ValidFrom: str(m["valid_from"]), ValidTo: str(m["valid_to"]),
		}
		if emb, ok := m["embedding"].([]any); ok {
			lf.Embedding = make([]float64, len(emb))
			for i, x := range emb {
				switch n := x.(type) {
				case float64:
					lf.Embedding[i] = n
				}
			}
		}
		out = append(out, lf)
	}
	return out, nil
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func firstStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(m[k]); s != "" {
			return s
		}
	}
	return ""
}
