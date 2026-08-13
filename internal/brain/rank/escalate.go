package rank

// SecondSource is the web-search block on a deduction answer.
// Kept apart from graph hits so "ours" and "not ours" stay visible.
type SecondSource struct {
	Status  string            `json:"status"`
	Note    string            `json:"note,omitempty"`
	Cached  bool              `json:"cached,omitempty"`
	Results []SecondSourceHit `json:"results,omitempty"`
}

type SecondSourceHit struct {
	Rank    int    `json:"rank"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Engine  string `json:"engine"`
}

type WebFn func(query string) SecondSource

// ShouldEscalate is true when the default deduction path has no facts hit.
// `--root facts|info` is a single-root ask: do not mix in the web.
func ShouldEscalate(hits []Hit, rootFilter string) bool {
	if rootFilter != "" {
		return false
	}
	for _, h := range hits {
		if h.Root == "facts" {
			return false
		}
	}
	return true
}

// Deduce returns the second-source block, or nil when web must not run.
func Deduce(hits []Hit, query, rootFilter string, noWeb bool, web WebFn) *SecondSource {
	if noWeb || web == nil || !ShouldEscalate(hits, rootFilter) {
		return nil
	}
	out := web(query)
	return &out
}
