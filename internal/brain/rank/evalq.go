package rank

// Eval control questions (recall@5). Kept here so CI can test the gate
// table without ladybug cgo. The runner lives in internal/brain (cgo).
const EvalRecallThreshold = 0.95

type EvalQuestion struct {
	Query    string
	Fragment string
}

var EvalQuestions = []EvalQuestion{
	{"hybrid search fts and vector", "BM25"},
	{"eslider devops engineer", "DevOps"},
	{"ladybugdb graph engine storage", "LadybugDB"},
}
