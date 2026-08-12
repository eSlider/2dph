// Ranking of hybrid search hits: reciprocal rank fusion plus the root/repo
// filters. Pure logic, no database and no model, so rank_test.go covers it.
package main

import (
	"sort"
	"strings"
)

// rrfK dampens the contribution of low ranks; same constant as kblib.py.
const rrfK = 60

// rankAndFilter fuses the two hit lists, applies the --root/--repo filters and
// only then cuts to limit. Cutting first dropped every matching leaf ranked
// below the cut in the unfiltered list, so `--root facts` came back empty
// whenever info leafs filled the top N. limit <= 0 keeps everything.
func rankAndFilter(fts, vec []Hit, root, repo string, limit int) []Hit {
	out := hybrid(fts, vec, 0)
	if root != "" {
		out = filterRoot(out, root)
	}
	if repo != "" {
		out = filterRepo(out, repo)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// hybrid merges FTS and vector hits by reciprocal rank fusion.
// limit <= 0 returns the full fused list.
func hybrid(fts, vec []Hit, limit int) []Hit {
	byID := make(map[string]Hit, len(fts)+len(vec))
	rrf := make(map[string]float64, len(fts)+len(vec))

	for rank, h := range fts {
		byID[h.ID] = h
		rrf[h.ID] += 1.0 / (rrfK + float64(rank+1))
	}
	for rank, h := range vec {
		if existing, ok := byID[h.ID]; !ok {
			byID[h.ID] = h
		} else if existing.Score == 0 {
			// FTS carried no score for this leaf; keep the cosine one.
			existing.Score = h.Score
			byID[h.ID] = existing
		}
		rrf[h.ID] += 1.0 / (rrfK + float64(rank+1))
	}

	ids := make([]string, 0, len(rrf))
	for id := range rrf {
		ids = append(ids, id)
	}
	// Ties broken by id so output never depends on map iteration order.
	sort.Slice(ids, func(i, j int) bool {
		if rrf[ids[i]] != rrf[ids[j]] {
			return rrf[ids[i]] > rrf[ids[j]]
		}
		return ids[i] < ids[j]
	})
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}

	out := make([]Hit, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func filterRoot(hits []Hit, root string) []Hit {
	var out []Hit
	for _, h := range hits {
		if h.Root == root {
			out = append(out, h)
		}
	}
	return out
}

func filterRepo(hits []Hit, repo string) []Hit {
	var out []Hit
	for _, h := range hits {
		if strings.Contains(h.Source, repo) {
			out = append(out, h)
		}
	}
	return out
}
