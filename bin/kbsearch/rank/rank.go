// Package rank is the cgo-free ranking and CLI parsing for kbsearch.
// CI can `go test ./rank` without the native ladybug library.
package rank

import (
	"sort"
	"strings"
)

// Hit is one search result, mirroring the python script's dict shape.
type Hit struct {
	ID      string  `json:"id"`
	Text    string  `json:"text"`
	Root    string  `json:"root"`
	Source  string  `json:"-"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

// rrfK dampens the contribution of low ranks; same constant as kblib.py.
const rrfK = 60

// RankAndFilter fuses the two hit lists, applies --root/--repo, then cuts to
// limit. Cutting first dropped every matching leaf ranked below the cut, so
// `--root facts` came back empty whenever info leafs filled the top N.
// limit <= 0 keeps everything.
func RankAndFilter(fts, vec []Hit, root, repo string, limit int) []Hit {
	out := Hybrid(fts, vec, 0)
	if root != "" {
		out = FilterRoot(out, root)
	}
	if repo != "" {
		out = FilterRepo(out, repo)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Hybrid merges FTS and vector hits by reciprocal rank fusion.
// limit <= 0 returns the full fused list.
func Hybrid(fts, vec []Hit, limit int) []Hit {
	byID := make(map[string]Hit, len(fts)+len(vec))
	rrf := make(map[string]float64, len(fts)+len(vec))

	for i, h := range fts {
		byID[h.ID] = h
		rrf[h.ID] += 1.0 / (rrfK + float64(i+1))
	}
	for i, h := range vec {
		if existing, ok := byID[h.ID]; !ok {
			byID[h.ID] = h
		} else if existing.Score == 0 {
			existing.Score = h.Score
			byID[h.ID] = existing
		}
		rrf[h.ID] += 1.0 / (rrfK + float64(i+1))
	}

	ids := make([]string, 0, len(rrf))
	for id := range rrf {
		ids = append(ids, id)
	}
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

func FilterRoot(hits []Hit, root string) []Hit {
	var out []Hit
	for _, h := range hits {
		if h.Root == root {
			out = append(out, h)
		}
	}
	return out
}

func FilterRepo(hits []Hit, repo string) []Hit {
	var out []Hit
	for _, h := range hits {
		if strings.Contains(h.Source, repo) {
			out = append(out, h)
		}
	}
	return out
}
