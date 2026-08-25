//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,facts_prove_crm "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && facts_prove_crm
//
// bin/facts/prove-crm.go - prove person->company and company->project associations.
//
// Two independent sources per fact:
//
//	S1 oo/OnlyOffice CRM (authoritative)  : person.company_id -> company,
//	                                        project.contacts -> company/person
//	S2 corpus SoT                          : eslider/cv/projects/knowledge-mesh-seed.yaml
//	                                        (orgs: employer/client/...  + projects)
//
// Only associations supported by BOTH sources are written as root=facts.
// Mismatches are reported. The two-source merge rule lives in
// internal/facts.CRMAssocFacts (single implementation, repo rule #10) — this
// CLI only feeds it the corpus orgs + ooCRM graph and writes the proven facts.
//
// Usage:
//
//	bin/facts/prove-crm.go                    write proven facts (needs var/kb.lbug)
//	bin/facts/prove-crm.go --dry-run          show proposed facts + mismatches only
//	bin/facts/prove-crm.go --mismatches       show associations found in only one side
//	bin/facts/prove-crm.go --mesh PATH        knowledge-mesh-seed.yaml
//	bin/facts/prove-crm.go --graph PATH       ooCRM graph.json
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/internal/facts"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		dry   bool
		mism  bool
		mesh  string
		graph string
	)
	p := cli.New("facts-crm")
	p.Description = "prove person->company and company->project associations (corpus x ooCRM)"
	p.Bool(&dry, "", "dry-run", "show proposed facts + mismatches only")
	p.Bool(&mism, "", "mismatches", "show associations found in only one side")
	p.String(&mesh, "", "mesh", "knowledge-mesh-seed.yaml (default $KNOWLEDGE_MESH_SEED or ../knowledge-mesh-seed.yaml)")
	p.String(&graph, "", "graph", "ooCRM graph.json (default /tmp/opencode/crm/graph.json)")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}

	meshPath := mesh
	if meshPath == "" {
		meshPath = os.Getenv("KNOWLEDGE_MESH_SEED")
	}
	if meshPath == "" {
		meshPath = filepath.Join(brain.RepoRoot(), "..", "knowledge-mesh-seed.yaml")
	}
	raw, err := os.ReadFile(meshPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/prove-crm: mesh: %v\n", err)
		return 1
	}
	orgs := facts.CorpusOrgs(string(raw))

	graphPath := graph
	if graphPath == "" {
		graphPath = "/tmp/opencode/crm/graph.json"
	}
	gdata, err := os.ReadFile(graphPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/prove-crm: graph: %v\n", err)
		return 1
	}
	var g facts.CRMAssoc
	if err := json.Unmarshal(gdata, &g); err != nil {
		fmt.Fprintf(os.Stderr, "facts/prove-crm: graph json: %v\n", err)
		return 1
	}

	// Two-source merge (corpus orgs x ooCRM graph) — single implementation in
	// internal/facts so the rule is covered by offline unit tests (#55).
	proven, mismatches := facts.CRMAssocFacts(orgs, g)

	fmt.Printf("# CRM association facts proven (corpus x CRM): %d\n", len(proven))
	for _, f := range proven {
		fmt.Println("  -", f)
	}
	fmt.Printf("# mismatches / one-sided associations: %d\n", len(mismatches))
	for _, f := range mismatches {
		fmt.Println("  !", f)
	}

	if dry {
		return 0
	}

	// Write proven facts into the brain (root=facts, 2 sources each).
	model, err := brain.LoadModel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/prove-crm: model: %v\n", err)
		return 1
	}
	defer model.Close()
	db, conn, err := brain.OpenWritable("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/prove-crm: open: %v\n", err)
		return 1
	}
	defer db.Close()
	defer conn.Close()

	statsBefore := 0
	if res, err := conn.Query("MATCH (l:Leaf) WHERE l.root='facts' RETURN count(*) AS n"); err == nil {
		for res.HasNext() {
			if row, err := res.Next(); err == nil {
				if vals, err := row.GetAsSlice(); err == nil && len(vals) > 0 {
					statsBefore = toInt(vals[0])
				}
			}
		}
	}
	rev := time.Now().Format("20060102-150405")
	src := fmt.Sprintf("ooCRM x %s", filepath.Base(meshPath))
	written := 0
	for _, f := range proven {
		emb, err := model.Embed(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "facts/prove-crm: embed: %v\n", err)
			return 1
		}
		if _, err := brain.UpsertLeaf(conn, brain.LeafInput{
			Text: f, Root: "facts", Confidence: "confirmed",
			Source: src, SourceRev: rev, How: "crm-crosscheck",
			Loc: "bin/facts/crm", Type: "association", Embedding: emb,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "facts/prove-crm: write: %v\n", err)
			return 1
		}
		written++
	}
	fmt.Printf("# wrote %d facts into var/kb.lbug (facts was %d)\n", written, statsBefore)
	return 0
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}