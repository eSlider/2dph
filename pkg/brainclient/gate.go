package brainclient

import (
	"github.com/eSlider/2dph/internal/contract"
)

// Гейт facts (P-9.5, docs/brain/read-contract.md): клиентский слой никогда
// не выдаёт не подтверждённый ответ как подтверждённый факт. Правило
// «≥2 независимых источника» соблюдается на записи (promote, internal/facts);
// здесь — оборона на чтении: root=facts + confidence confirmed (или legacy
// пустое) считается фактом, всё остальное помечается (not confirmed).
// Противоречия (2v2, D16) хранятся как hypothesis и никогда не проходят гейт.

// Gate — вердикт гейта для одного листа/хита: true = ответ можно подавать
// как подтверждённый факт. Подтверждён только root=facts; пустой confidence
// (legacy-лист) трактуется как confirmed по read-contract.md.
func Gate(root, confidence string) bool {
	if root != "facts" {
		return false // info и отсутствие корня — нарратив, не факт
	}
	switch confidence {
	case "", "confirmed":
		return true
	default:
		return false // hypothesis/partial/estimated/inferred/… — (not confirmed)
	}
}

// GateReason — пометка для не-факта (пустая, когда Gate вернул true).
// Клиент обязан показывать её рядом с ответом, чтобы агент не процитировал
// hypothesis/partial/info как факт.
func GateReason(root, confidence string) string {
	if Gate(root, confidence) {
		return ""
	}
	switch {
	case root != "facts":
		return "(not confirmed): root=" + root + " (нарратив, не факт)"
	case confidence == "hypothesis":
		return "(not confirmed): confidence=hypothesis (противоречие/недостаточно источников, D16)"
	default:
		return "(not confirmed): confidence=" + confidence
	}
}

// NotFact — хит, который гейт отклонил: плоские поля хита (JSON-схема
// P-9.4) + причина отклонения. Такие ответы клиент никогда не подаёт как
// подтверждённые факты.
type NotFact struct {
	contract.SearchHit
	Reason string `json:"reason,omitempty"`
}

// Facts — гейтнутый ответ поиска с root=facts: Confirmed содержит только
// подтверждённые факты, NotConfirmed — всё, что гейт отклонил, с причиной.
// Клиентская JSON-схема на базе search-ответа контракта: вместо results[]
// — confirmed[] / not_confirmed[] (поля хитов те же, P-9.4); сервер такой
// ответ не шлёт, это представление клиента.
type Facts struct {
	ContractVersion string               `json:"contract_version"`
	Query           string               `json:"query"`
	RootFilter      string               `json:"root_filter"` // "facts"
	AsOf            string               `json:"as_of,omitempty"`
	Count           int                  `json:"count"` // число подтверждённых фактов
	Confirmed       []contract.SearchHit `json:"confirmed"`
	NotConfirmed    []NotFact            `json:"not_confirmed,omitempty"`
	Web             *contract.WebBlock   `json:"web,omitempty"`
}

// GateSearch применяет гейт к поисковому ответу (любого root_filter):
// возвращает разделение на подтверждённые факты и отклонённое.
func GateSearch(r *contract.SearchResponse) *Facts {
	f := &Facts{
		ContractVersion: r.ContractVersion,
		Query:           r.Query,
		RootFilter:      r.RootFilter,
		AsOf:            r.AsOf,
		Web:             r.Web,
		Confirmed:       make([]contract.SearchHit, 0), // JSON: [] (не null)
		NotConfirmed:    make([]NotFact, 0),
	}
	for i := range r.Results {
		h := r.Results[i]
		if Gate(h.Root, h.Confidence) {
			f.Confirmed = append(f.Confirmed, h)
			continue
		}
		f.NotConfirmed = append(f.NotConfirmed, NotFact{SearchHit: h, Reason: GateReason(h.Root, h.Confidence)})
	}
	f.Count = len(f.Confirmed)
	return f
}

// AuditGate — гейт поверх audit-ответа (гистограмма root × confidence):
// сколько листов facts-корня подтверждено и сколько отклонено гейтом.
// NotConfirmedFacts > 0 значит: на facts-корне есть hypothesis/partial —
// такие листы не должны подаваться как факты (разбор: brain/audit-contract).
type AuditGate struct {
	ConfirmedFacts    int `json:"confirmed_facts"`
	NotConfirmedFacts int `json:"not_confirmed_facts"`
	TotalFacts        int `json:"total_facts"`
}

// GateAudit применяет гейт к аудит-гистограмме (строки считаются по count).
func GateAudit(a *contract.AuditResponse) AuditGate {
	var g AuditGate
	for _, row := range a.ByConfidence {
		if row.Root != "facts" {
			continue
		}
		g.TotalFacts += row.Count
		if Gate(row.Root, row.Confidence) {
			g.ConfirmedFacts += row.Count
		} else {
			g.NotConfirmedFacts += row.Count
		}
	}
	return g
}

// OK — гейт чист: на facts-корне нет не подтверждённого confidence.
func (g AuditGate) OK() bool { return g.NotConfirmedFacts == 0 }
