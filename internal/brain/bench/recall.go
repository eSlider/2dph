package bench

import (
	"strings"
)

// FragmentRecalled reports whether any of the top-k hits contains the
// expected fragment. Matching is case-insensitive; a multi-word fragment
// requires every word to appear (in any order) inside one hit's text, so
// fragments survive line breaks inside a leaf. A single-word fragment behaves
// exactly like the eval.go substring gate.
func FragmentRecalled(hits []Hit, fragment string, k int) bool {
	if k <= 0 || k > len(hits) {
		k = len(hits)
	}
	hits = hits[:k]
	fragment = strings.ToLower(strings.TrimSpace(fragment))
	if fragment == "" {
		return false
	}
	words := strings.Fields(fragment)
	for _, h := range hits {
		text := strings.ToLower(h.Text)
		ok := true
		for _, w := range words {
			if !strings.Contains(text, w) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// RecallResult is the recall@k metric for one k.
type RecallResult struct {
	K        int     `json:"k"`
	Recalled int     `json:"recalled"`
	Total    int     `json:"total"`
	Score    float64 `json:"score"`
}

func recallResult(k, recalled, total int) RecallResult {
	score := 0.0
	if total > 0 {
		score = round3(float64(recalled) / float64(total))
	}
	return RecallResult{K: k, Recalled: recalled, Total: total, Score: score}
}

// FragmentRecall computes recall@k over queries that carry a fragment:
// fraction of such queries whose fragment appears in their top-k hits.
// Queries without a fragment are skipped (they still count for latency).
func FragmentRecall(results []QueryResult, k int) RecallResult {
	recalled, total := 0, 0
	for _, r := range results {
		if strings.TrimSpace(r.Entry.Fragment) == "" {
			continue
		}
		total++
		if r.Err == nil && FragmentRecalled(r.Hits, r.Entry.Fragment, k) {
			recalled++
		}
	}
	return recallResult(k, recalled, total)
}

// BaselineRecall computes recall@k of a candidate against the baseline:
// fraction of the baseline top-k IDs that appear in the candidate's top-k.
// Missing candidate hits (fewer than k) count as misses. This is the A/B
// regression metric from issue #201: the candidate must not lose known hits.
func BaselineRecall(candidate, baseline []Hit, k int) RecallResult {
	if k > len(baseline) {
		k = len(baseline)
	}
	cand := make(map[string]bool, len(candidate))
	for _, h := range candidate {
		if len(cand) >= k {
			break
		}
		cand[h.ID] = true
	}
	recalled := 0
	for i, h := range baseline {
		if i >= k {
			break
		}
		if cand[h.ID] {
			recalled++
		}
	}
	return recallResult(k, recalled, k)
}

// TopIDs extracts the top-k hit IDs of a result (used to persist baseline
// truth for later candidate comparisons).
func TopIDs(hits []Hit, k int) []string {
	if k > len(hits) {
		k = len(hits)
	}
	out := make([]string, 0, k)
	for i := 0; i < k; i++ {
		out = append(out, hits[i].ID)
	}
	return out
}
