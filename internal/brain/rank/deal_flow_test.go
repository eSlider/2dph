package rank

import (
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/facts"
)

// deal_flow_test.go — issue #58 test plan. Verifies the `search -> get ->
// audit` operator flow offline on ONE known deal, plus the
// contradiction-as-(not confirmed)-until-audited path (D16).
//
// The brain's `get` step (lookupLeaf / HTTP.Get) is cgo+ladybug bound, so the
// offline test stands a fixture leaf-store in for the DB fetch: a synthetic
// deal leaf set (Alice/Bob/example.com, no PII) whose id -> leaf resolution is
// the same data a real `get` would return. All adjudication/lexicon logic under
// test (RankAndFilter, ConfirmedFact, ShouldEscalate, facts.CheckFactRow,
// facts.Adjudicate) is the real, production, cgo-free code.

// dealLeaf mirrors the brain leaf fields the `get` step exposes to the audit.
type dealLeaf struct {
	Text       string
	Root       string
	Confidence string
	Source     string // evidence pointer(s); facts use the "a x b vs c x d" lexicon
	Loc        string
	How        string
}

// acmeDeal is the synthetic "one known deal" fixture. Synthetic, no PII.
var acmeDeal = map[string]dealLeaf{
	"d-owner": {
		Text: "Alice is the technical owner of the Acme cloud deal",
		Root: "facts", Confidence: facts.ConfConfirmed,
		Source: "crm.md x contract.md", Loc: "corpus/acme/contract.md", How: "prove-crm",
	},
	"d-q3": {
		Text: "The Acme cloud deal closes in Q3 2026",
		Root: "facts", Confidence: facts.ConfConfirmed,
		Source: "crm.md x forecast.md", Loc: "corpus/acme/forecast.md", How: "prove-crm",
	},
	"d-contra": {
		Text: "Acme renews with the incumbent vendor",
		Root: "facts", Confidence: facts.ConfHypothesis,
		// 2 independent yes refs vs 2 independent no refs, no authority or
		// temporal rule fires -> stays (not confirmed) until audited.
		Source: "deck-a x talk-b vs blog-c x wiki-d", Loc: "corpus/acme/vendor.md", How: "contradict",
	},
	"d-notes": {
		Text: "Acme plan notes: cloud migration timeline",
		Root: "info", Confidence: "",
		Source: "notes.md", Loc: "corpus/acme/notes.md", How: "ingest",
	},
}

// getLeaf stands in for the DB-backed `get` step: resolve a leaf by id.
func getLeaf(id string) (dealLeaf, bool) {
	l, ok := acmeDeal[id]
	return l, ok
}

// ftsVecForDeal builds a plausible FTS+vector hit pair for the fixture so the
// search step fuses the same leafs a real hybrid query would return.
func ftsVecForDeal() (fts, vec []Hit) {
	fts = []Hit{
		{ID: "d-owner", Text: acmeDeal["d-owner"].Text, Root: "facts", Confidence: facts.ConfConfirmed, Source: "crm.md x contract.md"},
		{ID: "d-q3", Text: acmeDeal["d-q3"].Text, Root: "facts", Confidence: facts.ConfConfirmed, Source: "crm.md x forecast.md"},
		{ID: "d-contra", Text: acmeDeal["d-contra"].Text, Root: "facts", Confidence: facts.ConfHypothesis, Source: "deck-a x talk-b vs blog-c x wiki-d"},
		{ID: "d-notes", Text: acmeDeal["d-notes"].Text, Root: "info", Source: "notes.md"},
	}
	vec = []Hit{
		{ID: "d-owner", Text: acmeDeal["d-owner"].Text, Root: "facts", Confidence: facts.ConfConfirmed, Source: "crm.md x contract.md"},
		{ID: "d-notes", Text: acmeDeal["d-notes"].Text, Root: "info", Source: "notes.md"},
	}
	return fts, vec
}

// searchDeal runs the `search` step: fuse retrievers, filter to facts+info.
func searchDeal() []Hit {
	fts, vec := ftsVecForDeal()
	return RankAndFilter(fts, vec, "", "", 0)
}

// TestDealSearchGetAuditConfirmedFlow is the core #58 plan: for one known deal,
// search surfaces the confirmed facts + info, get exposes their evidence, and
// audit confirms the two-source facts while info stays out of the facts root.
func TestDealSearchGetAuditConfirmedFlow(t *testing.T) {
	// --- search ---
	hits := searchDeal()
	var ownerHit, q3Hit, notesHit *Hit
	for i := range hits {
		switch hits[i].ID {
		case "d-owner":
			ownerHit = &hits[i]
		case "d-q3":
			q3Hit = &hits[i]
		case "d-notes":
			notesHit = &hits[i]
		}
	}
	if ownerHit == nil || q3Hit == nil || notesHit == nil {
		t.Fatalf("search must surface owner/q3/notes, got %v", ids(hits))
	}
	// facts root, confirmed
	if !ConfirmedFact(*ownerHit) || !ConfirmedFact(*q3Hit) {
		t.Fatalf("owner/q3 must be confirmed facts: %+v %+v", ownerHit, q3Hit)
	}
	// info root, not a fact
	if notesHit.Root != "info" || ConfirmedFact(*notesHit) {
		t.Fatalf("notes must be info-only, not a fact: %+v", notesHit)
	}

	// --- get ---
	leaf, ok := getLeaf(ownerHit.ID)
	if !ok {
		t.Fatalf("get: no leaf %s", ownerHit.ID)
	}
	if leaf.Confidence != facts.ConfConfirmed || !strings.Contains(leaf.Source, " x ") {
		t.Fatalf("get: owner leaf must carry two-source evidence: %+v", leaf)
	}

	// --- audit (lexicon gate) ---
	if probs := facts.CheckFactRow(ownerHit.ID, leaf.Source, leaf.Loc, leaf.How, leaf.Confidence); len(probs) != 0 {
		t.Fatalf("audit: confirmed owner fact flagged: %v", probs)
	}

	// --- audit (adjudication) ---
	r := facts.Adjudicate(facts.Claim{
		Text: leaf.Text,
		Yes: []facts.Source{
			{ID: "crm.md", Kind: facts.KindConfig},
			{ID: "contract.md", Kind: facts.KindConfig},
		},
	})
	if !r.Confirmed || r.Rule != facts.RuleTwoSource {
		t.Fatalf("audit: owner claim must be confirmed two-source, got %+v", r)
	}

	// Confirmed facts present -> no web escalation (deduction already proven).
	if ShouldEscalate(hits, "") {
		t.Fatal("confirmed deal facts must suppress web escalation")
	}
}

// TestDealContradictionNotConfirmedUntilAudited proves the #58 contradiction
// path: a 2v2 claim with no firing rule is (not confirmed) until audited, and
// never surfaces as a confirmed fact through search.
func TestDealContradictionNotConfirmedUntilAudited(t *testing.T) {
	// --- search surfaces the contradiction leaf, but not as confirmed ---
	hits := searchDeal()
	var contraHit *Hit
	for i := range hits {
		if hits[i].ID == "d-contra" {
			contraHit = &hits[i]
		}
	}
	if contraHit == nil {
		t.Fatal("search must surface the contradiction leaf")
	}
	if ConfirmedFact(*contraHit) {
		t.Fatal("contradiction must never be reported as a confirmed fact")
	}

	// --- get ---
	leaf, ok := getLeaf("d-contra")
	if !ok {
		t.Fatal("get: no contradiction leaf")
	}

	// --- audit (lexicon shape) : well-formed contradiction passes the gate ---
	if probs := facts.CheckFactRow("d-contra", leaf.Source, leaf.Loc, leaf.How, leaf.Confidence); len(probs) != 0 {
		t.Fatalf("audit: well-formed contradiction flagged: %v", probs)
	}

	// --- audit (adjudication) : 2v2, no rule fires -> (not confirmed) ---
	yes := []facts.Source{
		{ID: "deck-a", Kind: facts.KindNarrative},
		{ID: "talk-b", Kind: facts.KindNarrative},
	}
	no := []facts.Source{
		{ID: "blog-c", Kind: facts.KindNarrative},
		{ID: "wiki-d", Kind: facts.KindNarrative},
	}
	r := facts.Adjudicate(facts.Claim{Text: leaf.Text, Yes: yes, No: no})
	if r.Confirmed {
		t.Fatalf("2v2 contradiction must stay (not confirmed), got %+v", r)
	}
	if r.Confidence != facts.ConfHypothesis || r.Rule != facts.RuleUnresolved {
		t.Fatalf("want hypothesis/unresolved, got %+v", r)
	}
	if r.Winner != "" {
		t.Fatalf("unresolved must not name a winner: %+v", r)
	}

	// Until audited/resolved, the deduction must escalate (not confirmed).
	if !ShouldEscalate([]Hit{*contraHit, {ID: "d-notes", Root: "info"}}, "") {
		t.Fatal("hypothesis + info only must escalate to the second source")
	}
}
