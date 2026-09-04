package brain

// Юнит-тесты L-9.4 (#233) на чистой логике (cgo-free): план записи
// git-коммита → Commit/Person узлы + ребро AUTHORED (Commit→Person).
// Реальная Ladybug не нужна — проверяем SQL-формы MERGE и деривацию автора;
// идемпотентность на живой БД — gitcommit_test.go (cgo).

import (
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/canon"
)

// commitFixture — synthetic git-коммит (без PII). Email уже нормализован
// коннектором (gitgraph.ToInputs); план устойчив и к сырому регистру.
func commitFixture() CommitInput {
	return CommitInput{
		ID:      "demo-repo:0123456789abcdef0123456789abcdef01234567",
		Repo:    "demo-repo",
		SHA:     "0123456789abcdef0123456789abcdef01234567",
		Subject: "feat: mesh node",
		Author:  "Ada Lovelace",
		Email:   "ada@example.com",
		Date:    "2026-08-10T12:00:00Z",
	}
}

// commitUpsertQuery: MERGE Commit по канону id (repo:sha), SET всех полей
// узла — повторный прогон не плодит дублей, свойства обновляются.
func TestCommitUpsertQueryShape(t *testing.T) {
	q := commitUpsertQuery()
	for _, frag := range []string{
		"MERGE (c:Commit {id:$id})",
		"SET c.repo=$repo", "c.subject=$subject", "c.author=$author",
		"c.email=$email", "c.date=$date",
	} {
		if !strings.Contains(q, frag) {
			t.Fatalf("commitUpsertQuery missing %q:\n%s", frag, q)
		}
	}
}

// authoredEdgeQuery: MATCH обоих концов (Commit и Person существуют) +
// MERGE ребра AUTHORED Commit→Person — идемпотентность на уровне запроса.
func TestAuthoredEdgeQueryShape(t *testing.T) {
	q := authoredEdgeQuery()
	for _, frag := range []string{
		"MATCH (c:Commit {id:$cid}), (p:Person {id:$pid})",
		"MERGE (c)-[:AUTHORED]->(p)",
	} {
		if !strings.Contains(q, frag) {
			t.Fatalf("authoredEdgeQuery missing %q:\n%s", frag, q)
		}
	}
}

// Автор коммита → Person-узел: id = email (lowercase канон, как mail D-1
// #257 — тот же email в git и mail = один узел), name — display name из
// git author. Регистр email нормализуется здесь же (страховка от клиентов).
func TestCommitAuthorDerivation(t *testing.T) {
	in := commitFixture()
	in.Email = "Ada@Example.COM"

	p, ok := commitAuthor(in)
	if !ok {
		t.Fatal("author with email must plan a Person")
	}
	want := canon.Person{ID: "ada@example.com", Name: "Ada Lovelace", Email: "ada@example.com"}
	if p != want {
		t.Fatalf("author = %+v, want %+v", p, want)
	}
}

// Коммит без email автора: Person не деривируется (сопряжение по email
// невозможно — AUTHORED не строится), Commit-узел при этом остаётся.
func TestCommitAuthorNoEmail(t *testing.T) {
	in := commitFixture()
	in.Email = ""

	if _, ok := commitAuthor(in); ok {
		t.Fatal("author without email must not plan a Person")
	}
}
