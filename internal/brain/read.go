//go:build cgo && system_ladybug

package brain

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/eSlider/2dph/internal/brain/rank"
	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/pkg/cli"
)

func MainGet(args []string) int {
	opt, err := rank.ParseGet(args)
	if err != nil {
		return cli.Fail(err)
	}
	id, body, jsonOut := opt.ID, opt.Body, opt.JSONOut
	if err := openBrain(); err != nil {
		fmt.Fprintf(os.Stderr, "open brain: %v\n", err)
		return 1
	}
	defer closeBrain()
	meta, text, err := lookupLeaf(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/get: %v\n", err)
		return 1
	}
	out := Dict{
		{"id", meta["id"]},
		{"root", meta["root"]},
		{"confidence", meta["confidence"]},
		{"source", meta["source"]},
		{"type", meta["type"]},
	}
	if meta["valid_from"] != "" {
		out = append(out, KV{"valid_from", meta["valid_from"]})
	}
	if meta["valid_to"] != "" {
		out = append(out, KV{"valid_to", meta["valid_to"]})
	}
	if body {
		out = append(out, KV{"text", text})
	} else {
		out = append(out, KV{"snippet", clip(text, 280)})
	}
	if jsonOut {
		m := map[string]any{}
		for _, kv := range out {
			m[kv.K] = kv.V
		}
		m["contract_version"] = contract.ReadContractVersion
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return b2i(enc.Encode(m))
	}
	fmt.Print(toYAML(out, 0))
	return 0
}

func MainStats(args []string) int {
	opt, err := rank.ParseJSONFlag("brain-stats", args)
	if err != nil {
		return cli.Fail(err)
	}
	jsonOut := opt.JSONOut
	if err := openBrain(); err != nil {
		fmt.Fprintf(os.Stderr, "open brain: %v\n", err)
		return 1
	}
	defer closeBrain()
	s, err := leafStats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/stats: %v\n", err)
		return 1
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return b2i(enc.Encode(s))
	}
	by := s["by_root"].(map[string]int)
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	byRoot := make(Dict, 0, len(keys))
	for _, k := range keys {
		byRoot = append(byRoot, KV{k, by[k]})
	}
	out := Dict{
		{"total", s["total"]},
		{"by_root", byRoot},
		{"db", s["db"]},
		{"model", s["model"]},
		{"index_at", s["index_at"]},
		{"import_at", s["import_at"]},
		{"kb_mtime", s["kb_mtime"]},
		{"stale", s["stale"]},
	}
	fmt.Print(toYAML(out, 0))
	return 0
}

func MainEval(args []string) int {
	opt, err := rank.ParseJSONFlag("brain-eval", args)
	if err != nil {
		return cli.Fail(err)
	}
	jsonOut := opt.JSONOut
	if err := openBrain(); err != nil {
		fmt.Fprintf(os.Stderr, "open brain: %v\n", err)
		return 1
	}
	defer closeBrain()
	recalled := 0
	details := make([]any, 0, len(rank.EvalQuestions))
	jsDetails := make([]map[string]any, 0, len(rank.EvalQuestions))
	for _, q := range rank.EvalQuestions {
		hits, err := queryFTS(q.Query, 5, rank.Filter{})
		ok := false
		if err == nil {
			frag := strings.ToLower(q.Fragment)
			for _, h := range hits {
				if strings.Contains(strings.ToLower(h.Text), frag) {
					ok = true
					break
				}
			}
		}
		if ok {
			recalled++
		}
		details = append(details, Dict{
			{"q", q.Query},
			{"fragment", q.Fragment},
			{"in_top5", ok},
		})
		jsDetails = append(jsDetails, map[string]any{
			"q": q.Query, "fragment": q.Fragment, "in_top5": ok,
		})
	}
	n := len(rank.EvalQuestions)
	recall := 0.0
	if n > 0 {
		recall = float64(recalled) / float64(n)
	}
	fRecalled, fTotal, fDetails, fJSDetails := evalFilteredGate()
	fRecall := 0.0
	if fTotal > 0 {
		fRecall = float64(fRecalled) / float64(fTotal)
	}
	// A corpus without the asked slice (e.g. no promoted facts yet) skips the
	// filtered cases: that is not a retrieval regression. When cases do run,
	// they are held to the same threshold as the unfiltered gate.
	passed := recall >= rank.EvalRecallThreshold &&
		(fTotal == 0 || fRecall >= rank.EvalRecallThreshold)
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(map[string]any{
			"recall@5":          round3(recall),
			"passed":            passed,
			"gate":              n,
			"details":           jsDetails,
			"filtered_recall@5": round3(fRecall),
			"filtered_gate":     fTotal,
			"filtered_details":  fJSDetails,
		})
	} else {
		out := Dict{
			{"recall@5", round3(recall)},
			{"passed", passed},
			{"gate", n},
			{"details", details},
			{"filtered_recall@5", round3(fRecall)},
			{"filtered_gate", fTotal},
			{"filtered_details", fDetails},
		}
		fmt.Print(toYAML(out, 0))
	}
	if !passed {
		return 2
	}
	return 0
}

// evalFilteredGate runs the filter-aware cases of rank.EvalFilteredQuestions
// (issue #3) through the real retrieval path (root/repo/as-of applied before
// the top-k cut). A case passes when the fragment survives the top-5 cut and
// every returned hit is eligible — a filter leak or an empty answer from a
// rare root both fail. Cases whose corpus slice is absent are skipped: an
// empty corpus is not a retrieval bug (the same honesty rule as
// "throttled ≠ absence"). Returns recalled, total and the per-case reports.
func evalFilteredGate() (int, int, []any, []map[string]any) {
	const k = 5
	recalled, total := 0, 0
	details := make([]any, 0, len(rank.EvalFilteredQuestions))
	jsDetails := make([]map[string]any, 0, len(rank.EvalFilteredQuestions))
	for _, q := range rank.EvalFilteredQuestions {
		rep := map[string]any{
			"q": q.Query, "fragment": q.Fragment, "root": q.Root,
			"repo": q.Repo, "as_of": q.AsOf,
		}
		if !corpusHasSlice(q) {
			rep["skipped"] = "no matching leafs in corpus"
			details = append(details, dictFromMap(rep))
			jsDetails = append(jsDetails, rep)
			continue
		}
		emb, embErr := embedQueryFn(q.Query)
		hits, err := searchHitsEmb(q.Query, emb, q.Filter(), k, false, false)
		if embErr != nil {
			rep["note"] = "fts-only (no embedding model)"
		}
		inTop, eligible := false, true
		if err != nil {
			rep["error"] = err.Error()
		} else {
			f := q.Filter()
			for i, h := range hits {
				if !f.Eligible(h.Root, h.Source, h.ValidFrom, h.ValidTo) {
					eligible = false
				}
				if i < k && strings.Contains(strings.ToLower(h.Text), strings.ToLower(q.Fragment)) {
					inTop = true
				}
			}
		}
		ok := err == nil && inTop && eligible
		rep["in_top5"] = inTop
		rep["eligible"] = eligible
		rep["hits"] = len(hits)
		total++
		if ok {
			recalled++
		}
		details = append(details, dictFromMap(rep))
		jsDetails = append(jsDetails, rep)
	}
	return recalled, total, details, jsDetails
}

// corpusHasSlice reports whether the corpus holds at least one leaf the case
// could retrieve: the asked root, or a source containing the asked repo.
func corpusHasSlice(q rank.EvalQuestion) bool {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return false
	}
	stmtText := "MATCH (l:Leaf) RETURN count(*)"
	params := map[string]any{}
	switch {
	case q.Root != "":
		stmtText = "MATCH (l:Leaf {root:$root}) RETURN count(*)"
		params["root"] = q.Root
	case q.Repo != "":
		stmtText = "MATCH (l:Leaf) WHERE contains(l.source, $repo) RETURN count(*)"
		params["repo"] = q.Repo
	default:
		return true
	}
	stmt, err := conn.Prepare(stmtText)
	if err != nil {
		return false
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, params)
	if err != nil {
		return false
	}
	defer res.Close()
	if !res.HasNext() {
		return false
	}
	row, err := res.Next()
	if err != nil {
		return false
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) == 0 {
		return false
	}
	return asInt(vals[0]) > 0
}

func dictFromMap(m map[string]any) Dict {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(Dict, 0, len(keys))
	for _, k := range keys {
		out = append(out, KV{k, m[k]})
	}
	return out
}

func lookupLeaf(id string) (map[string]string, string, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, "", fmt.Errorf("brain not open")
	}
	stmt, err := conn.Prepare(
		"MATCH (l:Leaf {id:$id}) RETURN l.id, l.text, l.root, l.confidence, l.source, l.type, l.valid_from, l.valid_to",
	)
	if err != nil {
		return nil, "", err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"id": id})
	if err != nil {
		return nil, "", err
	}
	defer res.Close()
	if !res.HasNext() {
		return nil, "", fmt.Errorf("no leaf %s", id)
	}
	row, err := res.Next()
	if err != nil {
		return nil, "", err
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) < 8 {
		return nil, "", fmt.Errorf("leaf row")
	}
	meta := map[string]string{
		"id":         fmt.Sprint(vals[0]),
		"root":       fmt.Sprint(vals[2]),
		"confidence": fmt.Sprint(vals[3]),
		"source":     fmt.Sprint(vals[4]),
		"type":       fmt.Sprint(vals[5]),
		"valid_from": nullStr(vals[6]),
		"valid_to":   nullStr(vals[7]),
	}
	return meta, fmt.Sprint(vals[1]), nil
}

func leafStats() (map[string]any, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, count(*)")
	if err != nil {
		return nil, err
	}
	defer res.Close()
	byRoot := map[string]int{}
	total := 0
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			continue
		}
		n := int(asInt(vals[1]))
		byRoot[fmt.Sprint(vals[0])] = n
		total += n
	}
	fresh := ViewFreshnessForDB(dbPath())
	return map[string]any{
		"total":            total,
		"by_root":          byRoot,
		"db":               dbPath(),
		"model":            ModelID,
		"index_at":         fresh.IndexAt,
		"import_at":        fresh.ImportAt,
		"kb_mtime":         fresh.KBMtime,
		"pack_mtime":       fresh.PackMtime,
		"stale":            fresh.Stale,
		"stale_after":      fresh.StaleAfter,
		"last_error":       fresh.LastError,
		"contract_version": contract.ReadContractVersion,
	}, nil
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func round3(f float64) float64 {
	return float64(int(f*1000+0.5)) / 1000
}
