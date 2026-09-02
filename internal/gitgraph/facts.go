package gitgraph

// Факт-слой L-9.4 (#233): из Commit/Person/AUTHORED узлов графа (пишет
// bin/git/graph.go) — дедуктивные факты «кто работал над <repo> когда» с
// audit cards по Vinogradov (docs/audit-recipes.md). Чистая логика без
// liblbug: чтение графа исполняет bin/git/facts.go (cgo), здесь — фильтр,
// группировка, детерминированная сортировка и вердикты.
//
// Границы дедукции (иначе fallacy):
//   - «что сделал» — гипотеза из subject; слабый/пустой subject → weaken+OPEN
//     («трогал файлы», не «что именно»);
//   - авторство по Author коммита; merge-коммиты в граф не пишутся
//     (gitlog.Log пропускает), co-author невидим — ограничение.

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// FactRow — одна строка выборки Commit-AUTHORED-Person из графа.
type FactRow struct {
	ID      string // repo:sha
	Repo    string
	Subject string
	Date    string // RFC3339
	Email   string // Person.id = email lowercase
	Name    string
}

// FactFilter — фильтры git-facts CLI.
type FactFilter struct {
	Repo   string    // пусто = все
	Author string    // email, пусто = все
	Since  time.Time // только коммиты с датой >=
}

// FactGroup — факт: person работал над repo; Rows отсортированы по дате ASC.
type FactGroup struct {
	Repo  string
	Email string
	Name  string
	Rows  []FactRow
}

// Fact — audit card по Vinogradov (claim/premises/inference/gaps/verdict).
type Fact struct {
	Repo      string    `json:"repo"`
	Person    string    `json:"person"` // name <email>
	Claim     string    `json:"claim"`
	Period    string    `json:"period"`
	Commits   int       `json:"commits"`
	Weak      int       `json:"weak"`
	Premises  []string  `json:"premises"`
	Inference string    `json:"inference"`
	Gaps      []string  `json:"gaps"`
	Verdict   string    `json:"verdict"`
	Rows      []FactRow `json:"rows,omitempty"` // --commits
}

// GroupFacts фильтрует строки (repo/author/since) и группирует по
// (repo, email): person работал над repo. Детерминизм: группа сортирует
// коммиты по дате ASC (tie-break id), группы — по (repo, email).
func GroupFacts(rows []FactRow, f FactFilter) []FactGroup {
	byKey := map[string]*FactGroup{}
	var order []string
	for _, r := range rows {
		if f.Repo != "" && r.Repo != f.Repo {
			continue
		}
		if f.Author != "" && !strings.EqualFold(r.Email, f.Author) {
			continue
		}
		if !f.Since.IsZero() {
			t, ok := parseRFC3339(r.Date)
			if !ok || t.Before(f.Since) {
				continue
			}
		}
		key := r.Repo + "\x00" + r.Email
		g := byKey[key]
		if g == nil {
			g = &FactGroup{Repo: r.Repo, Email: r.Email, Name: r.Name}
			byKey[key] = g
			order = append(order, key)
		}
		g.Rows = append(g.Rows, r)
	}
	groups := make([]FactGroup, 0, len(order))
	for _, k := range order {
		g := byKey[k]
		sortCommitRows(g.Rows)
		groups = append(groups, *g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Repo != groups[j].Repo {
			return groups[i].Repo < groups[j].Repo
		}
		return groups[i].Email < groups[j].Email
	})
	return groups
}

func sortCommitRows(rows []FactRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		ti, ai := parseRFC3339(rows[i].Date)
		tj, aj := parseRFC3339(rows[j].Date)
		if ai && aj && !ti.Equal(tj) {
			return ti.Before(tj)
		}
		if ai != aj {
			return ai // непарсящиеся даты в конец
		}
		return rows[i].ID < rows[j].ID
	})
}

// BuildFacts превращает группы в факты-карточки. Premises — первые 5 commit
// id группы (+счётчик остатка); verdict accept, либо weaken когда ВСЕ
// subject-ы слабые (только «трогал файлы»), partial weak → gap OPEN.
func BuildFacts(groups []FactGroup, showCommits bool) []Fact {
	out := make([]Fact, 0, len(groups))
	for _, g := range groups {
		person := g.Name
		if person == "" {
			person = g.Email
		}
		weak := 0
		for _, r := range g.Rows {
			if WeakSubject(r.Subject) {
				weak++
			}
		}
		period := spanDay(g.Rows)
		f := Fact{
			Repo:      g.Repo,
			Person:    fmt.Sprintf("%s <%s>", person, g.Email),
			Period:    period,
			Commits:   len(g.Rows),
			Weak:      weak,
			Inference: "deduction",
			Claim:     fmt.Sprintf("«%s» работал(а) над %s в %s (%d коммитов)", person, g.Repo, period, len(g.Rows)),
		}
		if showCommits {
			f.Rows = g.Rows
		}
		maxPrem := 5
		if len(g.Rows) < maxPrem {
			maxPrem = len(g.Rows)
		}
		for i := 0; i < maxPrem; i++ {
			f.Premises = append(f.Premises, g.Rows[i].ID)
		}
		if len(g.Rows) > maxPrem {
			f.Premises = append(f.Premises, fmt.Sprintf("… +%d commits", len(g.Rows)-maxPrem))
		}
		switch {
		case weak == len(g.Rows):
			f.Verdict = "weaken"
			f.Gaps = append(f.Gaps, fmt.Sprintf("OPEN: все %d коммитов без содержательного message — выводится только «трогал файлы в %s», не «что именно»", weak, g.Repo))
		case weak > 0:
			f.Verdict = "accept"
			f.Gaps = append(f.Gaps, fmt.Sprintf("OPEN: %d из %d коммитов со служебным message — их «что именно» не выводится", weak, len(g.Rows)))
		default:
			f.Verdict = "accept"
		}
		out = append(out, f)
	}
	return out
}

// spanDay возвращает период группы «YYYY-MM-DD..YYYY-MM-DD» (один день —
// просто дата); строки уже отсортированы по дате.
func spanDay(rows []FactRow) string {
	if len(rows) == 0 {
		return ""
	}
	first, last := day(rows[0].Date), day(rows[len(rows)-1].Date)
	if first == last {
		return first
	}
	return first + ".." + last
}

func day(rfc3339 string) string {
	if len(rfc3339) >= 10 {
		return rfc3339[:10]
	}
	return rfc3339
}
