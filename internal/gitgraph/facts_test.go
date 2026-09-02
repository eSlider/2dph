package gitgraph

// Юнит-тесты факт-слоя L-9.4 (#233): фильтр/группировка строк
// Commit-AUTHORED-Person, детерминированная сортировка, audit cards
// (claim/premises/verdict weaken+OPEN на слабых subject-ах). Данные
// synthetic, БД не нужна.

import (
	"testing"
	"time"
)

func factRows() []FactRow {
	return []FactRow{
		{ID: "2dph:bbbb", Repo: "2dph", Subject: "fix: typo", Date: "2026-08-11T09:30:00Z", Email: "bob@example.com", Name: "Bob Babbage"},
		{ID: "2dph:aaaa", Repo: "2dph", Subject: "feat: mesh node", Date: "2026-08-10T12:00:00+01:00", Email: "ada@example.com", Name: "Ada Lovelace"},
		{ID: "2dph:cccc", Repo: "2dph", Subject: "Update", Date: "2026-08-12T08:00:00Z", Email: "ada@example.com", Name: "Ada Lovelace"},
		{ID: "gator:dddd", Repo: "gator", Subject: "wip", Date: "2026-08-13T10:00:00Z", Email: "ada@example.com", Name: "Ada Lovelace"},
	}
}

// Группировка по (repo, email): кто работал над чем; внутри группы коммиты
// по дате ASC (время, не лексикография строк: 12:00+01:00 == 11:00Z).
func TestGroupFactsGroupsAndSorts(t *testing.T) {
	groups := GroupFacts(factRows(), FactFilter{})
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3 (2dph/ada, 2dph/bob, gator/ada)", len(groups))
	}
	// порядок групп: repo asc, email asc → 2dph/ada, 2dph/bob, gator/ada
	if groups[0].Repo != "2dph" || groups[0].Email != "ada@example.com" {
		t.Fatalf("groups[0] = %+v", groups[0])
	}
	if groups[1].Repo != "2dph" || groups[1].Email != "bob@example.com" {
		t.Fatalf("groups[1] = %+v", groups[1])
	}
	ada := groups[0]
	if len(ada.Rows) != 2 || ada.Rows[0].ID != "2dph:aaaa" || ada.Rows[1].ID != "2dph:cccc" {
		t.Fatalf("ada rows order = %+v", ada.Rows)
	}
}

// Фильтры: repo, author (case-insensitive email), since (по дате коммита).
func TestGroupFactsFilters(t *testing.T) {
	rows := factRows()

	if got := GroupFacts(rows, FactFilter{Repo: "gator"}); len(got) != 1 {
		t.Fatalf("repo filter: %d groups", len(got))
	}
	if got := GroupFacts(rows, FactFilter{Author: "ADA@Example.COM"}); len(got) != 2 {
		t.Fatalf("author filter (case-insens): %d groups", len(got))
	}
	since := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	got := GroupFacts(rows, FactFilter{Since: since})
	if len(got) != 2 {
		t.Fatalf("since filter: %d groups", len(got))
	}
	// ada в 2dph после since: только «Update» 08-12 (слабый → weaken)
	ada := got[0]
	if ada.Email != "ada@example.com" || len(ada.Rows) != 1 || ada.Rows[0].Subject != "Update" {
		t.Fatalf("since-filtered ada = %+v", ada)
	}
}

// Audit card: claim содержит repo+период, premises — commit id (первые 5 +
// счётчик), inference=deduction. Полностью слабая группа (gator/ada: wip) →
// verdict weaken + gap OPEN.
func TestBuildFactsVerdicts(t *testing.T) {
	groups := GroupFacts(factRows(), FactFilter{})
	facts := BuildFacts(groups, false)
	if len(facts) != 3 {
		t.Fatalf("facts = %d", len(facts))
	}
	// 2dph/ada: 1 сильный + 1 слабый → accept, gap OPEN про слабый
	ada := facts[0]
	if ada.Verdict != "accept" {
		t.Fatalf("ada verdict = %q, want accept", ada.Verdict)
	}
	if ada.Claim == "" || ada.Inference != "deduction" || ada.Period != "2026-08-10..2026-08-12" {
		t.Fatalf("ada card = %+v", ada)
	}
	if len(ada.Premises) != 2 || ada.Premises[0] != "2dph:aaaa" {
		t.Fatalf("ada premises = %v", ada.Premises)
	}
	if len(ada.Gaps) == 0 {
		t.Fatal("ada must carry an OPEN gap about the weak commit")
	}
	// 2dph/bob: сильный → accept без gaps
	bob := facts[1]
	if bob.Verdict != "accept" || len(bob.Gaps) != 0 {
		t.Fatalf("bob card = %+v", bob)
	}
	// gator/ada: единственный коммит wip → weaken + OPEN
	gator := facts[2]
	if gator.Verdict != "weaken" || gator.Period != "2026-08-13" {
		t.Fatalf("gator card = %+v", gator)
	}
	if len(gator.Gaps) == 0 {
		t.Fatal("weaken card must carry an OPEN gap")
	}
}

// Premises ограничены 5 id + счётчик остатка; --commits отдаёт строки.
func TestBuildFactsPremisesLimitAndRows(t *testing.T) {
	var rows []FactRow
	for i := 0; i < 7; i++ {
		rows = append(rows, FactRow{
			ID:   "2dph:000000000000000000000000000000000000000" + string(rune('0'+i)),
			Repo: "2dph", Subject: "feat: x", Date: "2026-08-01T00:00:00Z", Email: "ada@example.com",
		})
	}
	groups := GroupFacts(rows, FactFilter{})
	facts := BuildFacts(groups, true)
	if len(facts) != 1 {
		t.Fatalf("facts = %d", len(facts))
	}
	f := facts[0]
	if len(f.Premises) != 6 { // 5 id + "+2 commits"
		t.Fatalf("premises = %d: %v", len(f.Premises), f.Premises)
	}
	if f.Premises[5] != "… +2 commits" {
		t.Fatalf("premises tail = %q", f.Premises[5])
	}
	if len(f.Rows) != 7 {
		t.Fatalf("rows (--commits) = %d", len(f.Rows))
	}
}

// Person без display name — карточка по email.
func TestBuildFactsNoName(t *testing.T) {
	rows := []FactRow{{
		ID: "2dph:eeee", Repo: "2dph", Subject: "feat: x",
		Date: "2026-08-01T00:00:00Z", Email: "nobody@example.com", Name: "",
	}}
	facts := BuildFacts(GroupFacts(rows, FactFilter{}), false)
	if len(facts) != 1 {
		t.Fatalf("facts = %d", len(facts))
	}
	if facts[0].Person != "nobody@example.com <nobody@example.com>" {
		t.Fatalf("person = %q", facts[0].Person)
	}
}
