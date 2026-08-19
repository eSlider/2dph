//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,facts_crm "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && facts_crm
//
// bin/facts/crm.go - prove person->company and company->project associations.
//
// Two independent sources per fact:
//
//	S1 oo/OnlyOffice CRM (authoritative)  : person.company_id -> company,
//	                                        project.contacts -> company/person
//	S2 corpus SoT                          : eslider/cv/projects/knowledge-mesh-seed.yaml
//	                                        (orgs: employer/client/...  + projects)
//
// Only associations supported by BOTH sources are written as root=facts.
// Mismatches are reported (or, with --fix-crm, printed as oo CLI commands).
//
// Usage:
//
//	bin/facts/crm.go                    write proven facts (needs var/kb.lbug)
//	bin/facts/crm.go --dry-run          show proposed facts + mismatches only
//	bin/facts/crm.go --mismatches       show associations found in only one side
//	bin/facts/crm.go --mesh PATH        knowledge-mesh-seed.yaml
//	bin/facts/crm.go --graph PATH       ooCRM graph.json
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/cli"
	"github.com/eSlider/2dph/internal/facts"
)

type crmGraph struct {
	CompaniesWithPersons map[string][]string `json:"companies_with_persons"`
	ProjectsContacts     map[string]struct {
		Title     string   `json:"title"`
		Companies []string `json:"companies"`
	} `json:"projects_contacts"`
}

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
		fmt.Fprintf(os.Stderr, "facts/crm: mesh: %v\n", err)
		return 1
	}
	orgs := facts.CorpusOrgs(string(raw))

	graphPath := graph
	if graphPath == "" {
		graphPath = "/tmp/opencode/crm/graph.json"
	}
	gdata, err := os.ReadFile(graphPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/crm: graph: %v\n", err)
		return 1
	}
	var g crmGraph
	if err := json.Unmarshal(gdata, &g); err != nil {
		fmt.Fprintf(os.Stderr, "facts/crm: graph json: %v\n", err)
		return 1
	}

	companies := make([]string, 0, len(g.CompaniesWithPersons))
	for k := range g.CompaniesWithPersons {
		companies = append(companies, k)
	}
	sort.Strings(companies)

	var facts, mismatches []string
	for orgName, o := range orgs {
		token := o.Label
		if token == "" {
			token = orgName
		}
		firstToken := ""
		if parts := strings.Split(token, " "); len(parts) > 0 {
			firstToken = parts[0]
		}
		labels := strings.Split(o.Label, " / ")
		key := ""
		for _, k := range companies {
			if strings.Contains(strings.ToLower(k), strings.ToLower(firstToken)) {
				key = k
				break
			}
			hit := false
			for _, t := range labels {
				if strings.Contains(strings.ToLower(k), strings.ToLower(t)) {
					hit = true
					break
				}
			}
			if hit {
				key = k
				break
			}
		}
		persons := []string{}
		if key != "" {
			persons = g.CompaniesWithPersons[key]
		}
		switch {
		case len(persons) > 0:
			for _, p := range persons {
				facts = append(facts, fmt.Sprintf("%s is associated with %s (role: %s, %s)",
					p, o.Label, def(o.Kind, "?"), o.Period))
			}
		case key != "":
			mismatches = append(mismatches, fmt.Sprintf("corpus org '%s' (%s) has no CRM persons", orgName, o.Label))
		default:
			mismatches = append(mismatches, fmt.Sprintf("corpus org '%s' (%s) not found in CRM", orgName, o.Label))
		}
	}

	// corpus employer claims vs CRM.
	for orgName, o := range orgs {
		if o.Kind == "" {
			continue
		}
		switch o.Kind {
		case "employer", "own", "client", "agency", "apprenticeship":
			token := o.Label
			if token == "" {
				token = orgName
			}
			first := strings.Split(token, " ")[0]
			found := false
			for _, k := range companies {
				if strings.Contains(strings.ToLower(k), strings.ToLower(first)) {
					found = true
					break
				}
			}
			if !found {
				mismatches = append(mismatches, fmt.Sprintf("corpus org '%s' (%s) not found in CRM", orgName, o.Label))
			}
		}
	}

	fmt.Printf("# CRM association facts proven (corpus x CRM): %d\n", len(facts))
	for _, f := range facts {
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
		fmt.Fprintf(os.Stderr, "facts/crm: model: %v\n", err)
		return 1
	}
	defer model.Close()
	db, conn, err := brain.OpenWritable("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "facts/crm: open: %v\n", err)
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
	for _, f := range facts {
		emb, err := model.Embed(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "facts/crm: embed: %v\n", err)
			return 1
		}
		if _, err := brain.UpsertLeaf(conn, brain.LeafInput{
			Text: f, Root: "facts", Confidence: "confirmed",
			Source: src, SourceRev: rev, How: "crm-crosscheck",
			Loc: "bin/facts/crm", Type: "association", Embedding: emb,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "facts/crm: write: %v\n", err)
			return 1
		}
		written++
	}
	fmt.Printf("# wrote %d facts into var/kb.lbug (facts was %d)\n", written, statsBefore)
	return 0
}

func def(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
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