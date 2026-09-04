package facts

import (
	"fmt"
	"sort"
	"strings"
)

// L-9.3 (#232): формальные проверки Vinogradov на уровне URL. Правила:
//
//	identity          — один URL — один смысл: разные написания одного
//	                    канонического URL в фактах → merge/flag;
//	contradiction     — ¬(P ∧ ¬P) на одном URL: подтверждённые суждения
//	                    «URL: Attr» и «URL: ¬Attr» → audit flag verdict=reject;
//	excluded_middle   — P ∨ ¬P для чётких claim; слабое/неразложимое
//	                    суждение → не FACT, а open_questions[];
//	sufficient_reason — вердикт только на FACT-/OPEN- premises; нет premises
//	                    → reject/weaken, не accept.
//
// Суждение о ресурсе URL несёт Attr (предикат) и знак Affirm (P/¬P);
// Claim — человекочитаемый текст для карточек. Пустой Attr = claim не
// разложен на P/¬P (нечёткий). Conf — подтверждение leaf'а (закон
// исключённого третьего опирается на confirmed-сторону).

// Имена правил формальных проверок (L-9.3 #232).
const (
	RuleIdentity         = "identity"
	RuleContradiction    = "contradiction"
	RuleExcludedMiddle   = "excluded_middle"
	RuleSufficientReason = "sufficient_reason"
)

// URLFact — одна факт-строка URL-проверки: факт о ресурсе по URL
// (суждение Vinogradov: «ресурс URL имеет/не имеет признак Attr»).
type URLFact struct {
	ID    string `json:"id"`             // FACT-… / OPEN-…
	URL   string `json:"url"`            // субъект — URL ресурса (вакансия); пусто = не URL-факт
	Claim string `json:"claim"`          // человекочитаемый текст суждения («вакансия активна»)
	Attr  string `json:"attr"`           // предикат суждения («вакансия открыта»); "" = не разложен
	Neg   bool   `json:"neg"`            // true = утверждается ¬P, false = утверждается P
	Conf  string `json:"conf"`           // подтверждение: ConfConfirmed|ConfHypothesis|ConfPartial
	From  string `json:"from,omitempty"` // D24: начало действия (YYYY-MM-DD, пусто = всегда)
	To    string `json:"to,omitempty"`   // D24: конец действия (YYYY-MM-DD, пусто = всегда)
}

// Crisp — суждение чёткое (P∨¬P разрешимо): подтверждено и разложено на
// предикат. Слабое подтверждение (hypothesis/partial) или пустой Attr —
// нечёткое суждение (candidate open_questions).
func (f URLFact) Crisp() bool {
	if f.Attr == "" {
		return false
	}
	return f.Conf == "" || f.Conf == ConfConfirmed
}

// Problem — одна находка формальной проверки (audit flag, L-9.3 #232).
// Verdict — рекомендуемый вердикт аудит-карточки для этой находки.
type Problem struct {
	Rule    string   `json:"rule"`               // identity|contradiction|excluded_middle|sufficient_reason
	URL     string   `json:"url,omitempty"`      // канонический URL (для URL-правил)
	FactIDs []string `json:"fact_ids,omitempty"` // вовлечённые факты (отсортированы)
	Verdict Verdict  `json:"verdict"`            // accept|reject|weaken
	Message string   `json:"message"`
}

// sortStrings сортирует срез строк по месту (детерминизм находок).
func sortStrings(ss []string) {
	sort.Strings(ss)
}

// canonicalOrErr возвращает канонический URL факта; ok=false когда факт не
// несёт URL (пусто) — такие строки вне URL-проверок. Неканонизируемый URL
// возвращается ошибкой (identity-нарушение: ссылка битая).
func canonicalOrErr(f URLFact) (canon string, hasURL bool, err error) {
	if strings.TrimSpace(f.URL) == "" {
		return "", false, nil
	}
	canon, err = CanonicalURL(f.URL)
	return canon, true, err
}

// CheckURLIdentity — закон тождества: один канонический URL должен писаться
// одним способом. Два факта на один URL с разным написанием → merge/flag
// (weaken). Битая (неканонизируемая) ссылка — тоже identity-нарушение.
func CheckURLIdentity(facts []URLFact) []Problem {
	type group struct {
		canon string
		ids   []string
		raws  map[string]bool
	}
	byCanon := map[string]*group{}
	var order []string
	add := func(f URLFact, canon string) {
		g := byCanon[canon]
		if g == nil {
			g = &group{canon: canon, raws: map[string]bool{}}
			byCanon[canon] = g
			order = append(order, canon)
		}
		g.ids = append(g.ids, f.ID)
		g.raws[strings.TrimSpace(f.URL)] = true
	}
	var ps []Problem
	for _, f := range facts {
		canon, hasURL, err := canonicalOrErr(f)
		if err != nil {
			ps = append(ps, Problem{
				Rule: RuleIdentity, URL: f.URL, FactIDs: []string{f.ID},
				Verdict: VerdictWeaken,
				Message: fmt.Sprintf("identity: URL факта %s не канонизируется: %v", f.ID, err),
			})
			continue
		}
		if hasURL {
			add(f, canon)
		}
	}
	for _, canon := range order {
		g := byCanon[canon]
		if len(g.raws) < 2 {
			continue
		}
		ids := append([]string(nil), g.ids...)
		sortStrings(ids)
		ps = append(ps, Problem{
			Rule: RuleIdentity, URL: canon, FactIDs: ids,
			Verdict: VerdictWeaken,
			Message: fmt.Sprintf("identity: один URL (%s) записан в %d разных написаниях — свести к каноническому (merge/flag)", canon, len(g.raws)),
		})
	}
	sortProblems(ps)
	return ps
}

// CheckURLContradiction — закон противоречия ¬(P ∧ ¬P) в одном отношении:
// подтверждённые (confirmed) суждения об одном предикате одного URL с
// противоположными знаками и пересекающимися D24-интервалами →
// contradiction → verdict=reject. Слабые стороны (hypothesis) в противоречие
// не вступают — они уходят в open_questions (excluded middle).
func CheckURLContradiction(facts []URLFact) []Problem {
	type key struct{ url, attr string }
	type side struct {
		pos, neg []URLFact // P и ¬P
	}
	groups := map[key]*side{}
	var order []key
	add := func(f URLFact, k key) {
		s := groups[k]
		if s == nil {
			s = &side{}
			groups[k] = s
			order = append(order, k)
		}
		if f.Neg {
			s.neg = append(s.neg, f)
		} else {
			s.pos = append(s.pos, f)
		}
	}
	for _, f := range facts {
		if !f.Crisp() {
			continue // hypothesis/partial/неразложимое — не факт, см. excluded middle
		}
		canon, hasURL, err := canonicalOrErr(f)
		if err != nil || !hasURL {
			continue
		}
		add(f, key{canon, f.Attr})
	}
	var ps []Problem
	for _, k := range order {
		s := groups[k]
		if len(s.pos) == 0 || len(s.neg) == 0 {
			continue
		}
		// противоречие только при пересечении D24-интервалов
		if !anyOverlap(s.pos, s.neg) {
			continue
		}
		var ids []string
		for _, f := range append(append([]URLFact{}, s.pos...), s.neg...) {
			ids = append(ids, f.ID)
		}
		sortStrings(ids)
		ps = append(ps, Problem{
			Rule: RuleContradiction, URL: k.url, FactIDs: ids,
			Verdict: VerdictReject,
			Message: fmt.Sprintf("contradiction: %s: подтверждены и P и ¬P («%s» vs «%s»)", k.attr, s.pos[0].Claim, s.neg[0].Claim),
		})
	}
	sortProblems(ps)
	return ps
}

// anyOverlap — есть ли пара (pos, neg) с пересекающимися D24-интервалами.
func anyOverlap(pos, neg []URLFact) bool {
	for _, a := range pos {
		for _, b := range neg {
			if intervalsOverlap(a.From, a.To, b.From, b.To) {
				return true
			}
		}
	}
	return false
}

// intervalsOverlap — пересекаются ли два D24-интервала [f1,t1] и [f2,t2]
// (включительно; пустая граница = открытый интервал). Даты YYYY-MM-DD
// сравниваются лексикографически (канон D24).
func intervalsOverlap(f1, t1, f2, t2 string) bool {
	f1, t1 = NormalizeDay(f1), NormalizeDay(t1)
	f2, t2 = NormalizeDay(f2), NormalizeDay(t2)
	// [f1,t1] целиком раньше [f2,t2]
	if t1 != "" && f2 != "" && t1 < f2 {
		return false
	}
	// [f2,t2] целиком раньше [f1,t1]
	if t2 != "" && f1 != "" && t2 < f1 {
		return false
	}
	return true
}

// CheckExcludedMiddle — закон исключённого третьего: FACT-строка обязана
// быть чётким суждением (P∨¬P). Слабое подтверждение (hypothesis/partial)
// или claim без разложения на предикат — не факт: кандидат в
// open_questions[] (verdict=weaken).
func CheckExcludedMiddle(facts []URLFact) []Problem {
	var ps []Problem
	for _, f := range facts {
		if !strings.HasPrefix(f.ID, "FACT-") {
			continue // OPEN-… уже вопрос, не факт — не проверяем
		}
		if f.Crisp() {
			continue
		}
		why := "слабое подтверждение (не confirmed)"
		if f.Conf == "" || f.Conf == ConfConfirmed {
			why = "claim не разложен на P/¬P (нет предиката)"
		}
		ps = append(ps, Problem{
			Rule: RuleExcludedMiddle, URL: f.URL, FactIDs: []string{f.ID},
			Verdict: VerdictWeaken,
			Message: fmt.Sprintf("excluded_middle: %s: %s — не FACT, кандидат в open_questions[] (P∨¬P не разрешено)", f.ID, why),
		})
	}
	sortProblems(ps)
	return ps
}

// CheckSufficientReason — закон достаточного основания: verdict accept
// допустим только когда premises ссылаются на FACT-…/OPEN-…; без premises
// (или с не-FACT премисами) вердикт обязан быть reject/weaken, не accept.
// Возвращает проблемы для карточки; пусто = карточка достаточна.
func CheckSufficientReason(c AuditCard) []Problem {
	if c.Verdict != VerdictAccept {
		return nil // reject/weaken без premises закон позволяет
	}
	var ids []string
	if c.ID != "" {
		ids = []string{c.ID}
	}
	if len(c.Premises) == 0 {
		return []Problem{{
			Rule: RuleSufficientReason, FactIDs: ids,
			Verdict: VerdictReject,
			Message: "sufficient_reason: verdict accept без premises — только reject/weaken",
		}}
	}
	var bad []string
	for _, p := range c.Premises {
		pp := strings.TrimSpace(p)
		if !strings.HasPrefix(pp, "FACT-") && !strings.HasPrefix(pp, "OPEN-") {
			bad = append(bad, p)
		}
	}
	if len(bad) > 0 {
		sortStrings(bad)
		return []Problem{{
			Rule: RuleSufficientReason, FactIDs: ids,
			Verdict: VerdictReject,
			Message: fmt.Sprintf("sufficient_reason: accept опирается на не-FACT/OPEN премисы %v — только reject/weaken", bad),
		}}
	}
	return nil
}

// CheckFormal — сводный прогон URL-проверок L-9.3 (#232): identity +
// contradiction + excluded_middle. Детерминированный порядок (правило, URL,
// факты). Sufficient-reason проверяется отдельно по AuditCard.
func CheckFormal(facts []URLFact) []Problem {
	ps := append([]Problem{},
		CheckURLIdentity(facts)...,
	)
	ps = append(ps, CheckURLContradiction(facts)...)
	ps = append(ps, CheckExcludedMiddle(facts)...)
	sortProblems(ps)
	return ps
}

// sortProblems — детерминированный порядок находок: правило, URL, факты.
func sortProblems(ps []Problem) {
	sort.SliceStable(ps, func(i, j int) bool {
		if ps[i].Rule != ps[j].Rule {
			return ps[i].Rule < ps[j].Rule
		}
		if ps[i].URL != ps[j].URL {
			return ps[i].URL < ps[j].URL
		}
		return strings.Join(ps[i].FactIDs, ",") < strings.Join(ps[j].FactIDs, ",")
	})
}
