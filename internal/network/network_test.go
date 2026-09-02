package network

// Юнит-тесты сети связей L-9.5 (#234): деривации Person↔Person из mail
// (SENT/TO/CC/BCC/REPLY_TO + thread_id) и Person↔Project (AUTHORED
// Commit.repo), premises, вердикты, ранжирование. cgo-free: на строках
// выборки, без liblbug. Фикстуры synthetic — Alice/Bob/Carol/Dave/example.com.

import (
	"testing"
	"time"
)

// fixtureRows — synthetic-граф: 4 Person, два треда, reply-цепь, общий
// получатель, commits Alice+Bob в одном repo, Carol — в другом.
//
//	mail: m1 Alice→Bob (t1, TO)           — прямой контакт Alice↔Bob
//	      m2 Bob→Alice (t1, TO, reply m1) — ответ (диалог)
//	      m3 Alice→Carol (t2, TO)         — одиночный контакт Alice↔Carol
//	      m4 Dave→Alice+Bob (t1, TO)      — для Alice↔Bob: общий получатель;
//	                                         для Alice↔Dave: прямой Dave→Alice
//	git:  alice в demo (2), bob в demo (1) — общий проект demo
//	      carol в other (1) — другой проект
func fixtureRows() Rows {
	mail := []MsgRow{
		{
			ID: "m1@ex", ThreadID: "t1", SentAt: "2026-01-10T09:00:00Z", GatorRef: "kind=mail#v-aaaa1111",
			Sender: "alice@example.com", To: []string{"bob@example.com"},
		},
		{
			ID: "m2@ex", ThreadID: "t1", SentAt: "2026-01-11T10:00:00Z", GatorRef: "kind=mail#v-bbbb2222",
			Sender: "bob@example.com", To: []string{"alice@example.com"}, ReplyTo: "m1@ex",
		},
		{
			ID: "m3@ex", ThreadID: "t2", SentAt: "2026-02-01T09:00:00Z", GatorRef: "kind=mail#v-cccc3333",
			Sender: "alice@example.com", To: []string{"carol@example.com"},
		},
		{
			ID: "m4@ex", ThreadID: "t1", SentAt: "2026-01-12T09:00:00Z", GatorRef: "kind=mail#v-dddd4444",
			Sender: "dave@example.com", To: []string{"alice@example.com", "bob@example.com"},
		},
	}
	commits := []CommitRow{
		{ID: "demo:aaa1111111111111111111111111111111111111111", Repo: "demo", Date: "2026-03-01T10:00:00Z", Email: "alice@example.com"},
		{ID: "demo:bbb2222222222222222222222222222222222222222", Repo: "demo", Date: "2026-03-02T10:00:00Z", Email: "alice@example.com"},
		{ID: "demo:ccc3333333333333333333333333333333333333333", Repo: "demo", Date: "2026-03-03T10:00:00Z", Email: "bob@example.com"},
		{ID: "other:ddd4444444444444444444444444444444444444444", Repo: "other", Date: "2026-03-04T10:00:00Z", Email: "carol@example.com"},
	}
	persons := []PersonRow{
		{ID: "alice@example.com", Name: "Alice"},
		{ID: "bob@example.com", Name: "Bob"},
		{ID: "carol@example.com", Name: "Carol"},
		{ID: "dave@example.com", Name: "Dave"},
	}
	return Rows{Msgs: mail, Commits: commits, Persons: persons}
}

func day(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// Alice ↔ Bob: 2 прямых письма (m1 Alice→Bob, m2 Bob→Alice) + 1 общий
// получатель (m4) в одном треде, reply-цепь (m2 отвечает на m1), общий
// проект demo — accept, вес > 2.
func TestBuildLinksMailReplyChain(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "alice@example.com"})
	bob := findLink(t, links, "bob@example.com")
	if bob == nil {
		t.Fatal("bob link missing")
	}
	if bob.Msgs != 2 {
		t.Fatalf("bob Msgs = %d, want 2 (m1,m2)", bob.Msgs)
	}
	if bob.SharedCC != 1 {
		t.Fatalf("bob SharedCC = %d, want 1 (m4 both recipients)", bob.SharedCC)
	}
	if bob.Threads != 1 {
		t.Fatalf("bob Threads = %d, want 1 (t1)", bob.Threads)
	}
	if bob.Replies != 1 {
		t.Fatalf("bob Replies = %d, want 1 (m2 REPLY_TO m1)", bob.Replies)
	}
	if bob.Verdict != "accept" {
		t.Fatalf("bob verdict = %q, want accept", bob.Verdict)
	}
	if bob.Weight <= 2 {
		t.Fatalf("bob weight = %v, want > 2", bob.Weight)
	}
	if bob.Period != "2026-01-10..2026-01-12" {
		t.Fatalf("bob period = %q", bob.Period)
	}
	// premises содержат письма диалога (m1,m2)
	if !hasPremise(bob, "m1@ex") || !hasPremise(bob, "m2@ex") {
		t.Fatalf("bob mail premises missing: %+v", bob.Premises)
	}
}

// Общий проект: Alice и Bob оба коммитили в demo → git-факт с premises на
// commit id Bob. Carol — только mail, проект другой.
func TestBuildLinksGitProjectAndPremises(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "alice@example.com"})
	bob := findLink(t, links, "bob@example.com")
	if bob == nil {
		t.Fatal("bob missing")
	}
	if len(bob.Projects) != 1 || bob.Projects[0].Repo != "demo" {
		t.Fatalf("bob projects = %+v, want [demo]", bob.Projects)
	}
	if !hasPremise(bob, "demo:ccc3333333333333333333333333333333333333333") {
		t.Fatalf("bob git premise missing: %+v", bob.Premises)
	}
	carol := findLink(t, links, "carol@example.com")
	if carol == nil {
		t.Fatal("carol missing")
	}
	if len(carol.Projects) != 0 {
		t.Fatalf("carol projects = %+v, want none (other repo)", carol.Projects)
	}
}

// Carol: одно письмо от Alice → одиночный контакт: weaken (не
// экспортируется в CRM) + gap OPEN.
func TestBuildLinksSingleContactWeaken(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "alice@example.com"})
	carol := findLink(t, links, "carol@example.com")
	if carol == nil {
		t.Fatal("carol missing")
	}
	if carol.Msgs != 1 {
		t.Fatalf("carol Msgs = %d, want 1", carol.Msgs)
	}
	if carol.Verdict != "weaken" {
		t.Fatalf("carol verdict = %q, want weaken (single contact)", carol.Verdict)
	}
	if len(carol.Gaps) == 0 {
		t.Fatal("carol must have an OPEN gap (single contact)")
	}
}

// Dave: письмо m4 Dave→Alice (Alice в TO) — прямой контакт, но одиночный
// (нет ответа, нет проекта) → weaken. SharedCC у Dave = 0: m4 для пары
// Alice↔Dave это письмо ОТ Dave, а не общий получатель.
func TestBuildLinksDaveDirectSingle(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "alice@example.com"})
	dave := findLink(t, links, "dave@example.com")
	if dave == nil {
		t.Fatal("dave missing")
	}
	if dave.Msgs != 1 || dave.SharedCC != 0 {
		t.Fatalf("dave Msgs=%d SharedCC=%d, want 1/0 (direct Dave→Alice)", dave.Msgs, dave.SharedCC)
	}
	if dave.Verdict != "weaken" {
		t.Fatalf("dave verdict = %q, want weaken", dave.Verdict)
	}
}

// Ответный ход: сеть с точки зрения Bob — Alice в ней, reply засчитан.
func TestBuildLinksReplyFromOtherSide(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "bob@example.com"})
	alice := findLink(t, links, "alice@example.com")
	if alice == nil {
		t.Fatal("alice missing from bob network")
	}
	if alice.Replies != 1 {
		t.Fatalf("alice Replies = %d, want 1", alice.Replies)
	}
}

// Ранжирование: bob (2 письма + reply + проект) выше carol/dave (1 письмо).
func TestBuildLinksRanking(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "alice@example.com"})
	if len(links) < 3 {
		t.Fatalf("links = %d, want >= 3 (bob,carol,dave)", len(links))
	}
	if links[0].Person != "bob@example.com" {
		t.Fatalf("top link = %s, want bob (most weight)", links[0].Person)
	}
}

// Фильтр --project demo: только связи с общим проектом demo (git-ось).
func TestBuildLinksProjectFilter(t *testing.T) {
	rows := fixtureRows()
	links := BuildLinks(rows, Filter{Person: "alice@example.com", Project: "demo"})
	if len(links) != 1 || links[0].Person != "bob@example.com" {
		t.Fatalf("project=demo links = %+v, want only bob", links)
	}
}

// Фильтр --since 2026-02-15: письмо m3 (02-01) и m1/m2/m4 (январь)
// отсекаются; bob остаётся по коммитам demo (март).
func TestBuildLinksSinceFilter(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "alice@example.com", Since: day("2026-02-15T00:00:00Z")})
	if len(links) != 1 || links[0].Person != "bob@example.com" {
		t.Fatalf("since 2026-02-15 links = %+v, want only bob (git demo)", links)
	}
	if links[0].Msgs != 0 {
		t.Fatalf("bob Msgs after since filter = %d, want 0 (mails in january)", links[0].Msgs)
	}
}

// Только accept экспортируются в CRM (ADR-0012 п.4): carol/dave weaken не
// попадают в экспорт.
func TestAcceptOnly(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "alice@example.com"})
	var accepts []Link
	for _, l := range links {
		if l.Verdict == "accept" {
			accepts = append(accepts, l)
		}
	}
	if len(accepts) != 1 || accepts[0].Person != "bob@example.com" {
		t.Fatalf("accept-only = %+v, want bob", accepts)
	}
}

// Целевой Person без связей → пусто, без паники.
func TestBuildLinksNoContacts(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "nobody@example.com"})
	if len(links) != 0 {
		t.Fatalf("links = %+v, want none", links)
	}
}

// Сеть НЕ выводит самого target как контакт (self-связь не считается).
func TestBuildLinksNoSelfLink(t *testing.T) {
	links := BuildLinks(fixtureRows(), Filter{Person: "alice@example.com"})
	for _, l := range links {
		if l.Person == "alice@example.com" {
			t.Fatalf("self-link present: %+v", l)
		}
	}
}

// Классификация связей (N-1.1 #268): поле Kind = person|company|service.
// Люди/компании проходят, сервис-аккаунты (noreply@github.com) — Kind
// service и в CRM-экспорт accept не попадают.
func TestBuildLinksKindClassification(t *testing.T) {
	rows := Rows{
		Persons: []PersonRow{
			{ID: "alice@example.com", Name: "Alice"},
			{ID: "bob@example.com", Name: "Bob Smith"},
			{ID: "noreply@github.com", Name: "GitHub"},
			{ID: "info@example.com", Name: "Example"},
		},
		Msgs: []MsgRow{
			{ID: "m1@ex", ThreadID: "t1", SentAt: "2026-01-10T09:00:00Z",
				Sender: "noreply@github.com", To: []string{"alice@example.com"}},
			{ID: "m2@ex", ThreadID: "t1", SentAt: "2026-01-11T09:00:00Z",
				Sender: "noreply@github.com", To: []string{"alice@example.com"}},
			{ID: "m3@ex", ThreadID: "t2", SentAt: "2026-02-01T09:00:00Z",
				Sender: "bob@example.com", To: []string{"alice@example.com"}},
			{ID: "m4@ex", ThreadID: "t2", SentAt: "2026-02-02T09:00:00Z",
				Sender: "alice@example.com", To: []string{"bob@example.com"}},
			{ID: "m5@ex", ThreadID: "t3", SentAt: "2026-03-01T09:00:00Z",
				Sender: "info@example.com", To: []string{"alice@example.com"}},
			{ID: "m6@ex", ThreadID: "t3", SentAt: "2026-03-02T09:00:00Z",
				Sender: "alice@example.com", To: []string{"info@example.com"}},
		},
	}
	links := BuildLinks(rows, Filter{Person: "alice@example.com"})
	if got := kindOf(links, "bob@example.com"); got != "person" {
		t.Fatalf("bob kind = %q, want person", got)
	}
	if got := kindOf(links, "noreply@github.com"); got != "service" {
		t.Fatalf("github kind = %q, want service", got)
	}
	if got := kindOf(links, "info@example.com"); got != "company" {
		t.Fatalf("info@example.com kind = %q, want company (role inbox)", got)
	}
	// сервисы исключаются из accept-экспорта в CRM
	var accepts []Link
	for _, l := range links {
		if l.Verdict == "accept" && l.Kind != "service" {
			accepts = append(accepts, l)
		}
	}
	for _, l := range accepts {
		if l.Kind == "service" {
			t.Fatalf("service link in accept export: %+v", l)
		}
	}
}

func kindOf(links []Link, email string) string {
	for _, l := range links {
		if l.Person == email {
			return l.Kind
		}
	}
	return ""
}

func findLink(t *testing.T, links []Link, email string) *Link {
	t.Helper()
	for i := range links {
		if links[i].Person == email {
			return &links[i]
		}
	}
	return nil
}

func hasPremise(l *Link, ref string) bool {
	for _, p := range l.Premises {
		if p.Ref == ref {
			return true
		}
	}
	return false
}

// Регрессия: период git-only связи не должен склеиваться в "a..b..c".
// Alice и Bob коммитили в demo в разные дни — период = объединение.
func TestBuildLinksGitOnlyPeriodSingleSpan(t *testing.T) {
	rows := Rows{
		Persons: []PersonRow{{ID: "alice@example.com", Name: "Alice"}, {ID: "bob@example.com", Name: "Bob"}},
		Commits: []CommitRow{
			{ID: "demo:aaa1111111111111111111111111111111111111111", Repo: "demo", Date: "2026-03-01T10:00:00Z", Email: "alice@example.com"},
			{ID: "demo:bbb2222222222222222222222222222222222222222", Repo: "demo", Date: "2026-03-05T10:00:00Z", Email: "bob@example.com"},
		},
	}
	links := BuildLinks(rows, Filter{Person: "alice@example.com"})
	if len(links) != 1 {
		t.Fatalf("links = %+v, want bob", links)
	}
	bob := links[0]
	if bob.Period != "2026-03-01..2026-03-05" {
		t.Fatalf("bob period = %q, want 2026-03-01..2026-03-05", bob.Period)
	}
	if bob.Verdict != "accept" {
		t.Fatalf("bob verdict = %q, want accept (shared project)", bob.Verdict)
	}
}

func TestMergePeriods(t *testing.T) {
	cases := []struct{ a, b, want string }{
		{"2026-01-01", "2026-01-02", "2026-01-01..2026-01-02"},
		{"2026-01-01..2026-01-10", "2026-01-05", "2026-01-01..2026-01-10"},
		{"2026-01-01..2026-01-10", "2026-02-01..2026-02-05", "2026-01-01..2026-02-05"},
		{"2026-01-01", "", "2026-01-01"},
		{"", "2026-01-01", "2026-01-01"},
		{"2026-01-01", "2026-01-01", "2026-01-01"},
	}
	for _, c := range cases {
		if got := mergePeriods(c.a, c.b); got != c.want {
			t.Fatalf("mergePeriods(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
