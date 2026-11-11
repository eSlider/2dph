package rank

import (
	"strings"

	"github.com/eSlider/2dph/internal/facts"
)

// Filter is the eligibility predicate of a search: --root, --repo and --as-of.
// Retrieval applies it to candidates *before* the top-k cut (issue #3): with
// facts at 0.09% of the live corpus, filtering after the cut dropped the one
// eligible leaf from limit*3 candidates and answered a false "(not confirmed)".
// Keep the semantics here so the ANN index, the FTS query and the final ranking
// share one definition.
type Filter struct {
	Root string // exact leaf root (facts|info); empty = any
	Repo string // substring of the leaf source, mirroring FilterRepo; empty = any
	AsOf string // YYYY-MM-DD, D24 validity interval; empty = ignore intervals
}

// Empty reports whether no filter is active (the fast unfiltered path).
func (f Filter) Empty() bool { return f.Root == "" && f.Repo == "" && f.AsOf == "" }

// Eligible reports whether one candidate row passes every active filter.
// root/source/validFrom/validTo are the row metadata as stored in the graph or
// in the ANN index. Empty intervals are legacy leafs and stay eligible (D24).
func (f Filter) Eligible(root, source, validFrom, validTo string) bool {
	if f.Root != "" && root != f.Root {
		return false
	}
	if f.Repo != "" && !strings.Contains(source, f.Repo) {
		return false
	}
	if f.AsOf != "" && !facts.ActiveAt(validFrom, validTo, f.AsOf) {
		return false
	}
	return true
}

// FilterHits keeps the hits eligible for f, preserving order. An empty filter
// returns the input unchanged.
func FilterHits(hits []Hit, f Filter) []Hit {
	if f.Empty() {
		return hits
	}
	out := make([]Hit, 0, len(hits))
	for _, h := range hits {
		if f.Eligible(h.Root, h.Source, h.ValidFrom, h.ValidTo) {
			out = append(out, h)
		}
	}
	return out
}

// CandidateFactor is the first over-fetch step over the caller's limit; the
// same ×3 the unfiltered path always used.
const CandidateFactor = 3

// CandidateCap bounds the doubling ladder so one pathological filter cannot
// turn a query into an unbounded fetch.
const CandidateCap = 2048

// EscalateCandidates fetches candidates with an escalating budget — limit*3,
// then doubling up to CandidateCap — and stops as soon as fetch yields at
// least limit eligible rows or reports that it has no more matches (more ==
// false: the retriever returned a short window, so a bigger budget would only
// repeat the same rows). fetch=nil or a non-positive limit is a no-op.
//
// Retrievers that push Filter down (ANN index, filtered FTS query) therefore
// still degrade gracefully: a short result can only mean the corpus truly has
// fewer eligible rows, and the caller may fall back to the exact scan instead
// of reporting an empty answer as fact.
func EscalateCandidates(limit int, fetch func(n int) (hits []Hit, more bool, err error)) ([]Hit, error) {
	if limit <= 0 || fetch == nil {
		return nil, nil
	}
	n := limit * CandidateFactor
	if n <= 0 {
		n = limit
	}
	var best []Hit
	for {
		got, more, err := fetch(n)
		if err != nil {
			return nil, err
		}
		if len(got) >= len(best) {
			best = got
		}
		if len(best) >= limit {
			return best, nil
		}
		if !more || n >= CandidateCap {
			return best, nil
		}
		n *= 2
		if n > CandidateCap {
			n = CandidateCap
		}
	}
}
