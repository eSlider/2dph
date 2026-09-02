//go:build cgo && system_ladybug

package brain

import (
	"fmt"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/canon"
)

// L-9.4 (#233): запись Commit/Person + AUTHORED в реальную Ladybug (temp DB
// по образцу graph_test.go/write_test.go). Фикстуры synthetic —
// Ada/Bob/example.com. Канон id Commit = repo:sha, Person.id = email.

func commitPairFixture() []CommitInput {
	return []CommitInput{
		{
			ID:      "demo-repo:1111111111111111111111111111111111111111",
			Repo:    "demo-repo",
			SHA:     "1111111111111111111111111111111111111111",
			Subject: "feat: mesh node",
			Author:  "Ada Lovelace",
			Email:   "ada@example.com",
			Date:    "2026-08-10T12:00:00Z",
		},
		{
			ID:      "demo-repo:2222222222222222222222222222222222222222",
			Repo:    "demo-repo",
			SHA:     "2222222222222222222222222222222222222222",
			Subject: "fix: typo",
			Author:  "Bob Babbage",
			Email:   "bob@example.com",
			Date:    "2026-08-11T09:30:00Z",
		},
	}
}

// Commit/Person узлы и рёбра AUTHORED записываются и перечитываются.
func TestUpsertCommitsWritesGraph(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	if err := UpsertCommits(conn, commitPairFixture()); err != nil {
		t.Fatal(err)
	}
	// повторный InitSchema на той же БД — идемпотентен
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}

	if got := graphCount(t, conn, `MATCH (c:Commit) RETURN count(c)`); got != 2 {
		t.Fatalf("Commit count = %d, want 2", got)
	}
	if got := graphCount(t, conn, `MATCH (p:Person) RETURN count(p)`); got != 2 {
		t.Fatalf("Person count = %d, want 2 (ada/bob)", got)
	}
	if got := graphCount(t, conn, `MATCH ()-[:AUTHORED]->() RETURN count(*)`); got != 2 {
		t.Fatalf("AUTHORED count = %d, want 2", got)
	}

	// свойства Commit-узла перечитываются
	res, err := conn.Query(`MATCH (c:Commit {id:'demo-repo:1111111111111111111111111111111111111111'})
		RETURN c.repo, c.subject, c.author, c.email, c.date`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if !res.HasNext() {
		t.Fatal("commit node not found")
	}
	row, err := res.Next()
	if err != nil {
		t.Fatal(err)
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) < 5 {
		t.Fatalf("commit row: %v %v", vals, err)
	}
	wantProps := []string{"demo-repo", "feat: mesh node", "Ada Lovelace", "ada@example.com", "2026-08-10T12:00:00Z"}
	for i, w := range wantProps {
		if fmt.Sprint(vals[i]) != w {
			t.Fatalf("commit prop %d = %q, want %q", i, vals[i], w)
		}
	}

	// AUTHORED указывает на реального Person (id=email)
	if got := graphCount(t, conn,
		`MATCH (:Commit {id:'demo-repo:2222222222222222222222222222222222222222'})-[:AUTHORED]->(:Person {id:'bob@example.com'}) RETURN count(*)`); got != 1 {
		t.Fatalf("bob AUTHORED missing (got %d)", got)
	}
}

// Повторный прогон той же пачки — 0 новых узлов и рёбер (MERGE по id).
func TestUpsertCommitsIdempotent(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	if err := UpsertCommits(conn, commitPairFixture()); err != nil {
		t.Fatal(err)
	}
	nodes := func() (int, int, int) {
		t.Helper()
		return graphCount(t, conn, `MATCH (c:Commit) RETURN count(c)`),
			graphCount(t, conn, `MATCH (p:Person) RETURN count(p)`),
			graphCount(t, conn, `MATCH ()-[r]->() RETURN count(r)`)
	}
	c1, p1, r1 := nodes()
	for i := 0; i < 2; i++ {
		if err := UpsertCommits(conn, commitPairFixture()); err != nil {
			t.Fatal(err)
		}
	}
	c2, p2, r2 := nodes()
	if c1 != c2 || p1 != p2 || r1 != r2 {
		t.Fatalf("dup run grew graph: commits %d→%d, persons %d→%d, rels %d→%d",
			c1, c2, p1, p2, r1, r2)
	}
	if c2 != 2 || p2 != 2 || r2 != 2 {
		t.Fatalf("graph wrong after rerun: commits=%d persons=%d rels=%d", c2, p2, r2)
	}
}

// Сопряжение Person mail ↔ git: тот же email в mail-графе (D-1 #257) и в
// git-коммите → один Person-узел; AUTHORED (Commit)→Person сосуществует с
// SENT (Person)→Message.
func TestUpsertCommitsPersonMailMerge(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	// mail-граф: alice отправляет письмо (Person создаётся mail-путём)
	msg := MessageInput{
		Message: canon.Message{
			ID:       "m1@example.com",
			ThreadID: "thread-1",
			Platform: "mail",
			From:     canon.Person{ID: "ada@example.com", Name: "Ada Lovelace", Email: "ada@example.com"},
			SentAt:   time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC),
		},
		Folder: "INBOX",
	}
	if err := UpsertMessage(conn, msg); err != nil {
		t.Fatal(err)
	}

	// git-коммит тем же автором (тот же email)
	if err := UpsertCommit(conn, commitPairFixture()[0]); err != nil {
		t.Fatal(err)
	}

	if got := graphCount(t, conn, `MATCH (p:Person {id:'ada@example.com'}) RETURN count(p)`); got != 1 {
		t.Fatalf("ada Person duplicated across mail+git (count %d)", got)
	}
	if got := graphCount(t, conn,
		`MATCH (:Commit)-[:AUTHORED]->(:Person {id:'ada@example.com'}) RETURN count(*)`); got != 1 {
		t.Fatalf("git AUTHORED missing (got %d)", got)
	}
	if got := graphCount(t, conn,
		`MATCH (:Person {id:'ada@example.com'})-[:SENT]->(:Message) RETURN count(*)`); got != 1 {
		t.Fatalf("mail SENT must survive git import (got %d)", got)
	}
	// name-мердж: git-автор с тем же email не плодит узел и не затирает name
	res, err := conn.Query(`MATCH (p:Person {id:'ada@example.com'}) RETURN p.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if !res.HasNext() {
		t.Fatal("ada node not found")
	}
	row, err := res.Next()
	if err != nil {
		t.Fatal(err)
	}
	vals, _ := row.GetAsSlice()
	if len(vals) < 1 || fmt.Sprint(vals[0]) != "Ada Lovelace" {
		t.Fatalf("ada name = %v", vals)
	}
}

// Коммит без email автора: Commit-узел пишется, AUTHORED не создаётся.
func TestUpsertCommitNoEmailNoAuthored(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	in := commitPairFixture()[0]
	in.Email = ""
	in.ID = "demo-repo:3333333333333333333333333333333333333333"
	in.SHA = "3333333333333333333333333333333333333333"
	if err := UpsertCommit(conn, in); err != nil {
		t.Fatal(err)
	}

	if got := graphCount(t, conn, `MATCH (c:Commit) RETURN count(c)`); got != 1 {
		t.Fatalf("Commit count = %d, want 1", got)
	}
	if got := graphCount(t, conn, `MATCH (p:Person) RETURN count(p)`); got != 0 {
		t.Fatalf("Person count = %d, want 0 (no email)", got)
	}
	if got := graphCount(t, conn, `MATCH ()-[:AUTHORED]->() RETURN count(*)`); got != 0 {
		t.Fatalf("AUTHORED count = %d, want 0", got)
	}
}

// Граф-таблицы сосуществуют с Leaf/Message: лист и письмо читаются после
// git-записи (имитация «leafs не раздуваются» на малом масштабе).
func TestCommitGraphCoexistsWithLeafAndMessage(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	if _, err := UpsertLeaf(conn, LeafInput{
		Text: "existing leaf git-graph-token", Source: "gitcommit-test.md",
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCommits(conn, commitPairFixture()); err != nil {
		t.Fatal(err)
	}

	if got := graphCount(t, conn, `MATCH (l:Leaf) RETURN count(l)`); got != 1 {
		t.Fatalf("Leaf count = %d, want 1 (Leaf data must survive git schema)", got)
	}
	if got := graphCount(t, conn, `MATCH (c:Commit) RETURN count(c)`); got != 2 {
		t.Fatalf("Commit count = %d, want 2", got)
	}
	if err := EnsureIndexes(conn); err != nil {
		t.Fatal(err)
	}
}
