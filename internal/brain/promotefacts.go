//go:build cgo && system_ladybug

package brain

import (
	"strings"

	lbug "github.com/LadybugDB/go-ladybug"
)

// SourceCount counts independent evidence sources in a leaf's source string.
//
// facts/extract encodes 2 independent sources joined by " x "
// (e.g. "docker ps x compose:compose.yaml"). A single source — a file path, a
// commit, a chat file — has no separator and counts as 1. Duplicate parts are
// not independent and collapse to one. An empty source has 0 sources.
func SourceCount(source string) int {
	parts := strings.Split(source, " x ")
	seen := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		seen[p] = struct{}{}
	}
	return len(seen)
}

// PromoteEligible returns the subset of confirmed info leafs that carry >=2
// independent sources, re-rooted to facts. Single-source and non-confirmed
// leafs stay in info. Evidence-first: never fabricate a source.
func PromoteEligible(leafs []LeafInput) []LeafInput {
	out := make([]LeafInput, 0, len(leafs))
	for _, lf := range leafs {
		if lf.Root != "" && lf.Root != "info" {
			continue
		}
		if lf.Confidence != "" && lf.Confidence != "confirmed" {
			continue
		}
		if SourceCount(lf.Source) < 2 {
			continue
		}
		lf.Root = "facts"
		if lf.How == "" || lf.How == "kb/index" {
			lf.How = "facts/promote"
		}
		out = append(out, lf)
	}
	return out
}

// EligibleInfoLeafs returns confirmed info leafs that carry >=2 independent
// sources (evidence-first). Read-only; used by PromoteFacts and the --dry-run
// path of bin/facts/promote.go.
func EligibleInfoLeafs(conn *lbug.Connection) ([]LeafInput, error) {
	stmt, err := conn.Prepare(
		"MATCH (l:Leaf) WHERE l.root=$r AND l.confidence=$c " +
			"RETURN l.id, l.text, l.root, l.confidence, l.source, l.source_rev, l.how, l.loc, l.type, l.valid_from, l.valid_to",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"r": "info", "c": "confirmed"})
	if err != nil {
		return nil, err
	}
	defer res.Close()

	leafs := make([]LeafInput, 0)
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 11 {
			continue
		}
		leafs = append(leafs, LeafInput{
			Text:       nullStr(vals[1]),
			Root:       nullStr(vals[2]),
			Confidence: nullStr(vals[3]),
			Source:     nullStr(vals[4]),
			SourceRev:  nullStr(vals[5]),
			How:        nullStr(vals[6]),
			Loc:        nullStr(vals[7]),
			Type:       nullStr(vals[8]),
			ValidFrom:  nullStr(vals[9]),
			ValidTo:    nullStr(vals[10]),
		})
	}
	return PromoteEligible(leafs), nil
}

// PromoteFacts scans confirmed info leafs, promotes those with >=2 independent
// sources to facts-root (evidence-first) and returns the number promoted.
//
// Idempotent: a promoted leaf is no longer root=info, so a re-run finds nothing
// to promote and creates no duplicates. Embeddings are preserved — UpsertLeaf
// only touches the SET columns and skips the embedding clause when the input
// carries none.
func PromoteFacts(conn *lbug.Connection) (int, error) {
	toPromote, err := EligibleInfoLeafs(conn)
	if err != nil {
		return 0, err
	}
	if len(toPromote) == 0 {
		return 0, nil
	}
	if _, err := AddLeafs(conn, toPromote); err != nil {
		return 0, err
	}
	return len(toPromote), nil
}
