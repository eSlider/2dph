package rank

// Eval control questions (recall@5). Kept here so CI can test the gate
// table without ladybug cgo. The runner lives in internal/brain (cgo).
const EvalRecallThreshold = 0.95

type EvalQuestion struct {
	Query    string
	Fragment string
	Root     string // optional --root filter (facts|info)
	Repo     string // optional --repo filter (source substring)
	AsOf     string // optional --as-of filter (D24, YYYY-MM-DD)
}

func (q EvalQuestion) Filter() Filter {
	return Filter{Root: q.Root, Repo: q.Repo, AsOf: q.AsOf}
}

// EvalQuestions is the unfiltered recall@5 gate. Do not add filters here: the
// unfiltered pipeline must stay measured on its own.
var EvalQuestions = []EvalQuestion{
	{Query: "hybrid search fts and vector", Fragment: "BM25"},
	{Query: "eslider devops engineer", Fragment: "DevOps"},
	{Query: "ladybugdb graph engine storage", Fragment: "LadybugDB"},
}

// EvalFilteredQuestions is the filter-aware gate (issue #3). Each case names a
// rare slice of the corpus (root=facts, a repo, an as-of day) and the fragment
// that must survive the top-5 cut; every returned hit must also be eligible,
// so a filter leak fails the gate too. The fragment of an unfiltered search
// sits far below the cut for these broad queries (measured live: "onlyoffice"
// fact at BM25 rank 43, "curaSoft" at 161), so the old
// "fetch limit*3, filter afterwards" behaviour returns zero facts here and
// fails the gate.
var EvalFilteredQuestions = []EvalQuestion{
	{Query: "onlyoffice", Fragment: "OnlyOffice", Root: "facts"},
	{Query: "curaSoft", Fragment: "CuraSoft", Root: "facts"},
	{Query: "dzh", Fragment: "DZH", Root: "facts"},
	{Query: "onlyoffice update file", Fragment: "OnlyOffice", Repo: "office-assistant"},
	{Query: "onlyoffice", Fragment: "OnlyOffice", Root: "facts", AsOf: "2026-01-01"},
}
