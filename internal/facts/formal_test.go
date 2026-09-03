package facts

import (
	"reflect"
	"testing"
)

// urlFact is a compact URLFact literal for L-9.3 tests: id,url,claim,
// attr,neg,conf,from,to.
func urlFact(id, u, claim, attr string, neg bool, conf, from, to string) URLFact {
	return URLFact{
		ID:    id,
		URL:   u,
		Claim: claim,
		Attr:  attr,
		Neg:   neg,
		Conf:  conf,
		From:  from,
		To:    to,
	}
}

// problemIDs возвращает отсортированные id фактов проблемы (для сверки).
func problemIDs(p Problem) []string {
	ids := append([]string(nil), p.FactIDs...)
	sortStrings(ids)
	return ids
}

// findProblem возвращает проблему по правилу (и, если задан, canonical URL),
// nil если её нет.
func findProblem(ps []Problem, rule, url string) *Problem {
	for i := range ps {
		if ps[i].Rule == rule && (url == "" || ps[i].URL == url) {
			return &ps[i]
		}
	}
	return nil
}

// TestCheckURLIdentityMerge — закон тождества (L-9.3 #232): один и тот же URL
// в разных написаниях в двух фактах → merge/flag (identity).
func TestCheckURLIdentityMerge(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://JOBS.example.com:443/vacancy/42?utm_source=gis", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("FACT-02", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
	}
	ps := CheckURLIdentity(facts)
	p := findProblem(ps, RuleIdentity, "https://jobs.example.com/vacancy/42")
	if p == nil {
		t.Fatalf("identity problem expected, got %+v", ps)
	}
	if p.Verdict != VerdictWeaken {
		t.Errorf("identity verdict = %q, want weaken", p.Verdict)
	}
	if got := problemIDs(*p); !reflect.DeepEqual(got, []string{"FACT-01", "FACT-02"}) {
		t.Errorf("identity ids = %v, want both facts", got)
	}
}

// TestCheckURLIdentityDistinctURLsNoFlag — разные URL не дают identity-флага.
func TestCheckURLIdentityDistinctURLsNoFlag(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("FACT-02", "https://jobs.example.com/vacancy/7", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
	}
	if ps := CheckURLIdentity(facts); len(ps) != 0 {
		t.Fatalf("distinct canonical urls must not flag identity: %+v", ps)
	}
}

// TestCheckURLContradictionAcceptance — acceptance #232: два факта с одним URL
// и противоречивыми claim («вакансия активна» vs «вакансия closed») → audit
// flag contradiction verdict=reject.
func TestCheckURLContradictionAcceptance(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("FACT-02", "https://jobs.example.com/vacancy/42", "вакансия closed", "вакансия открыта", true, ConfConfirmed, "", ""),
	}
	ps := CheckURLContradiction(facts)
	p := findProblem(ps, RuleContradiction, "https://jobs.example.com/vacancy/42")
	if p == nil {
		t.Fatalf("contradiction problem expected, got %+v", ps)
	}
	if p.Verdict != VerdictReject {
		t.Errorf("contradiction verdict = %q, want reject", p.Verdict)
	}
	if got := problemIDs(*p); !reflect.DeepEqual(got, []string{"FACT-01", "FACT-02"}) {
		t.Errorf("contradiction ids = %v, want both facts", got)
	}
}

// TestCheckURLContradictionSamePolarityNoFlag — два факта утверждают одно P
// (обе polarity совпадают) — это не противоречие (¬(P ∧ ¬P) не нарушено).
func TestCheckURLContradictionSamePolarityNoFlag(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("FACT-02", "https://jobs.example.com/vacancy/42", "вакансия активна (2-й источник)", "вакансия открыта", false, ConfConfirmed, "", ""),
	}
	if ps := CheckURLContradiction(facts); len(ps) != 0 {
		t.Fatalf("same polarity must not contradict: %+v", ps)
	}
}

// TestCheckURLContradictionDifferentAttrNoFlag — один URL, разные предикаты:
// суждения о разных отношениях не противоречат друг другу.
func TestCheckURLContradictionDifferentAttrNoFlag(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("FACT-02", "https://jobs.example.com/vacancy/42", "зп указана", "зарплата указана", true, ConfConfirmed, "", ""),
	}
	if ps := CheckURLContradiction(facts); len(ps) != 0 {
		t.Fatalf("different attrs must not contradict: %+v", ps)
	}
}

// TestCheckURLContradictionWeakSideNoFlag — слабая сторона (hypothesis) не
// создаёт contradiction: нечёткое суждение уходит в excluded-middle, не в
// факт-противоречие (P и ¬P оба должны быть confirmed).
func TestCheckURLContradictionWeakSideNoFlag(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("OPEN-01", "https://jobs.example.com/vacancy/42", "возможно, закрыта", "вакансия открыта", true, ConfHypothesis, "", ""),
	}
	if ps := CheckURLContradiction(facts); len(ps) != 0 {
		t.Fatalf("hypothesis side must not contradict: %+v", ps)
	}
}

// TestCheckURLContradictionDisjointIntervalsNoFlag — P и ¬P в разных
// D24-интервалах (вакансия активна до 31.07, закрыта с 01.08) не
// противоречат друг другу.
func TestCheckURLContradictionDisjointIntervalsNoFlag(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "2026-07-01", "2026-07-31"),
		urlFact("FACT-02", "https://jobs.example.com/vacancy/42", "вакансия closed", "вакансия открыта", true, ConfConfirmed, "2026-08-01", ""),
	}
	if ps := CheckURLContradiction(facts); len(ps) != 0 {
		t.Fatalf("disjoint d24 intervals must not contradict: %+v", ps)
	}
}

// TestCheckURLContradictionOverlappingIntervalsFlag — пересекающиеся
// интервалы при противоположных суждениях — contradiction.
func TestCheckURLContradictionOverlappingIntervalsFlag(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "2026-07-01", ""),
		urlFact("FACT-02", "https://jobs.example.com/vacancy/42", "вакансия closed", "вакансия открыта", true, ConfConfirmed, "2026-07-15", ""),
	}
	ps := CheckURLContradiction(facts)
	if findProblem(ps, RuleContradiction, "https://jobs.example.com/vacancy/42") == nil {
		t.Fatalf("overlapping contradiction expected, got %+v", ps)
	}
}

// TestCheckExcludedMiddleFuzzyToOpenQuestions — нечёткое суждение (слабое
// подтверждение / неразложимый claim) не может быть FACT: кандидат в
// open_questions[] (закон исключённого третьего).
func TestCheckExcludedMiddleFuzzyToOpenQuestions(t *testing.T) {
	facts := []URLFact{
		// слабое подтверждение: hypothesis → не FACT
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия, возможно, активна", "вакансия открыта", false, ConfHypothesis, "", ""),
		// claim не разложен на P/¬P: Attr пуст → нечёткий
		urlFact("FACT-02", "https://jobs.example.com/vacancy/7", "что-то там с вакансией", "", false, ConfConfirmed, "", ""),
	}
	ps := CheckExcludedMiddle(facts)
	if len(ps) != 2 {
		t.Fatalf("2 fuzzy facts expected, got %+v", ps)
	}
	for _, p := range ps {
		if p.Rule != RuleExcludedMiddle || p.Verdict != VerdictWeaken {
			t.Errorf("excluded middle problem = %+v, want rule=excluded_middle verdict=weaken", p)
		}
		if len(p.FactIDs) != 1 {
			t.Errorf("excluded middle ids = %v, want one fact", p.FactIDs)
		}
	}
}

// TestCheckExcludedMiddleCrispFactsOK — чёткие confirmed-суждения с
// предикатом проходят excluded-middle.
func TestCheckExcludedMiddleCrispFactsOK(t *testing.T) {
	facts := []URLFact{
		urlFact("FACT-01", "https://jobs.example.com/vacancy/42", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("FACT-02", "https://jobs.example.com/vacancy/42", "вакансия закрыта", "вакансия открыта", true, ConfConfirmed, "", ""),
	}
	if ps := CheckExcludedMiddle(facts); len(ps) != 0 {
		t.Fatalf("crisp facts must pass excluded-middle: %+v", ps)
	}
}

// TestCheckSufficientReasonAcceptNeedsPremises — закон достаточного
// основания: verdict accept допустим только с FACT-/OPEN- premises; без
// premises (или с не-FACT премисами) — только reject/weaken.
func TestCheckSufficientReasonAcceptNeedsPremises(t *testing.T) {
	base := AuditCard{Date: "2026-09-04", Claim: "вакансия активна", Inference: InferenceDeduction, Counter: "none"}

	// accept с FACT-премисой — ок
	ok := base
	ok.Verdict = VerdictAccept
	ok.Premises = []string{"FACT-9001"}
	if ps := CheckSufficientReason(ok); len(ps) != 0 {
		t.Fatalf("accept with FACT premise must pass: %v", ps)
	}

	// accept без premises — нарушение (только reject/weaken)
	noPrem := base
	noPrem.Verdict = VerdictAccept
	if ps := CheckSufficientReason(noPrem); len(ps) == 0 {
		t.Fatal("accept without premises must fail sufficient reason")
	}

	// accept с премисой не FACT-/OPEN- — нарушение
	badPrem := base
	badPrem.Verdict = VerdictAccept
	badPrem.Premises = []string{"brain /stats: by_root facts=21"}
	if ps := CheckSufficientReason(badPrem); len(ps) == 0 {
		t.Fatal("accept with non-FACT premise must fail sufficient reason")
	}

	// reject/weaken без premises — закон позволяет (reject/weaken, не accept)
	rej := base
	rej.Verdict = VerdictReject
	if ps := CheckSufficientReason(rej); len(ps) != 0 {
		t.Fatalf("reject without premises is legal: %v", ps)
	}
	weak := base
	weak.Verdict = VerdictWeaken
	if ps := CheckSufficientReason(weak); len(ps) != 0 {
		t.Fatalf("weaken without premises is legal: %v", ps)
	}
}

// TestCheckFormalAggregates — сводная проверка: identity+contradiction+
// excluded-middle в одном вызове, детерминированный порядок.
func TestCheckFormalAggregates(t *testing.T) {
	facts := []URLFact{
		// identity: один URL в двух написаниях
		urlFact("FACT-01", "https://jobs.example.com/vac/1?utm_source=x", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("FACT-02", "https://jobs.example.com/vac/1", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		// contradiction: активна vs closed на одном URL
		urlFact("FACT-03", "https://jobs.example.com/vac/2", "вакансия активна", "вакансия открыта", false, ConfConfirmed, "", ""),
		urlFact("FACT-04", "https://jobs.example.com/vac/2", "вакансия closed", "вакансия открыта", true, ConfConfirmed, "", ""),
		// excluded middle: hypothesis → OPEN
		urlFact("FACT-05", "https://jobs.example.com/vac/3", "вакансия, вероятно, открыта", "вакансия открыта", false, ConfHypothesis, "", ""),
	}
	ps := CheckFormal(facts)
	rules := map[string]bool{}
	for _, p := range ps {
		rules[p.Rule] = true
	}
	for _, want := range []string{RuleIdentity, RuleContradiction, RuleExcludedMiddle} {
		if !rules[want] {
			t.Errorf("CheckFormal missing %q rule in %+v", want, ps)
		}
	}
	if len(ps) != 3 {
		t.Errorf("CheckFormal = %d problems, want 3 (identity+contradiction+excluded middle)", len(ps))
	}
}
