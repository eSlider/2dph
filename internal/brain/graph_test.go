//go:build cgo && system_ladybug

package brain

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/canon"
)

// D-1.2 (#259): запись Message/Person + рёбра в реальную Ladybug (temp DB по
// образцу write_test.go). Фикстуры synthetic — Alice/Bob/example.com.

// mailPairFixture — родитель + ответ (REPLY_TO), From/To/CC полный.
func mailPairFixture() []MessageInput {
	parent := "parent@example.com"
	return []MessageInput{
		{
			Message: canon.Message{
				ID:       parent,
				ThreadID: "thread-1",
				Platform: "mail",
				From:     canon.Person{ID: "alice@example.com", Name: "Alice", Email: "alice@example.com"},
				To:       []canon.Person{{ID: "bob@example.com", Name: "Bob", Email: "bob@example.com"}},
				SentAt:   time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC),
				Body:     "thread start",
			},
			Folder:   "INBOX",
			Subject:  "thread",
			GatorRef: "kind=mail#v-aaaa1111",
		},
		{
			Message: canon.Message{
				ID:       "child@example.com",
				ThreadID: "thread-1",
				Platform: "mail",
				From:     canon.Person{ID: "bob@example.com", Name: "Bob", Email: "bob@example.com"},
				ReplyTo:  &parent,
				To:       []canon.Person{{ID: "alice@example.com", Name: "Alice", Email: "alice@example.com"}},
				CC:       []canon.Person{{ID: "carol@example.com", Name: "Carol", Email: "carol@example.com"}},
				SentAt:   time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC),
				Body:     "reply body",
			},
			Folder:   "INBOX",
			Subject:  "Re: thread",
			GatorRef: "kind=mail#v-bbbb2222",
		},
	}
}

// graphCount исполняет Cypher-подсчёт (одна RETURN count(...) колонка).
func graphCount(t *testing.T, conn *lbug.Connection, query string) int {
	t.Helper()
	res, err := conn.Query(query)
	if err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	defer res.Close()
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			t.Fatalf("row %q: %v", query, err)
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			t.Fatalf("count row %q: %v", query, err)
		}
		n, err := strconv.Atoi(fmt.Sprint(vals[0]))
		if err != nil {
			t.Fatalf("count value %q: %v", vals[0], err)
		}
		return n
	}
	return 0
}

// Message/Person узлы и все рёбра записываются и перечитываются.
func TestUpsertMessagesWritesGraph(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	msgs := mailPairFixture()
	if err := UpsertMessages(conn, msgs); err != nil {
		t.Fatal(err)
	}

	// повторный InitSchema на той же БД — идемпотентен (уже зовётся в openTestDB)
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}

	if got := graphCount(t, conn, `MATCH (m:Message) RETURN count(m)`); got != 2 {
		t.Fatalf("Message count = %d, want 2", got)
	}
	if got := graphCount(t, conn, `MATCH (p:Person) RETURN count(p)`); got != 3 {
		t.Fatalf("Person count = %d, want 3 (alice/bob/carol)", got)
	}
	wantRels := map[string]int{
		"SENT":     2, // alice→parent, bob→child
		"TO":       2, // parent→bob, child→alice
		"CC":       1, // child→carol
		"BCC":      0,
		"REPLY_TO": 1, // child→parent
	}
	for rel, want := range wantRels {
		got := graphCount(t, conn, `MATCH ()-[r:`+rel+`]->() RETURN count(r)`)
		if got != want {
			t.Fatalf(":%s count = %d, want %d", rel, got, want)
		}
	}

	// REPLY_TO указывает на реального родителя
	if got := graphCount(t, conn,
		`MATCH (:Message {id:'child@example.com'})-[:REPLY_TO]->(:Message {id:'parent@example.com'}) RETURN count(*)`); got != 1 {
		t.Fatalf("child→parent REPLY_TO missing (got %d)", got)
	}

	// свойства узла Message перечитываются (gator_ref/folder/subject/body)
	res, err := conn.Query(
		`MATCH (m:Message {id:'child@example.com'}) RETURN m.folder, m.subject, m.gator_ref, m.body, m.sent_at, m.thread_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if !res.HasNext() {
		t.Fatal("child message node not found")
	}
	row, err := res.Next()
	if err != nil {
		t.Fatal(err)
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) < 6 {
		t.Fatalf("message row: %v %v", vals, err)
	}
	wantProps := []string{"INBOX", "Re: thread", "kind=mail#v-bbbb2222", "reply body", "2026-08-23T10:00:00Z", "thread-1"}
	for i, w := range wantProps {
		if fmt.Sprint(vals[i]) != w {
			t.Fatalf("message prop %d = %q, want %q", i, vals[i], w)
		}
	}

	// Person-узлы несут email
	if got := graphCount(t, conn,
		`MATCH (p:Person {id:'carol@example.com', email:'carol@example.com'}) RETURN count(p)`); got != 1 {
		t.Fatalf("carol person node with email missing (got %d)", got)
	}
}

// Повторный прогон той же пачки — 0 новых узлов и рёбер (MERGE по id).
func TestUpsertMessagesIdempotent(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	msgs := mailPairFixture()
	if err := UpsertMessages(conn, msgs); err != nil {
		t.Fatal(err)
	}
	nodes := func() (int, int, int) {
		t.Helper()
		return graphCount(t, conn, `MATCH (m:Message) RETURN count(m)`),
			graphCount(t, conn, `MATCH (p:Person) RETURN count(p)`),
			graphCount(t, conn, `MATCH ()-[r]->() RETURN count(r)`)
	}
	m1, p1, r1 := nodes()
	if err := UpsertMessages(conn, msgs); err != nil {
		t.Fatal(err)
	}
	if err := UpsertMessages(conn, msgs); err != nil {
		t.Fatal(err)
	}
	m2, p2, r2 := nodes()
	if m1 != m2 || p1 != p2 || r1 != r2 {
		t.Fatalf("dup run grew graph: messages %d→%d, persons %d→%d, rels %d→%d",
			m1, m2, p1, p2, r1, r2)
	}
	if m2 != 2 || p2 != 3 {
		t.Fatalf("graph wrong after rerun: messages=%d persons=%d", m2, p2)
	}
}

// Person name-мердж: тот же email в новом письме перезаписывает name
// последним значением (последний sync побеждает), дублей узла нет.
func TestUpsertMessagesPersonNameMerge(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	base := mailPairFixture()[0]
	first := base
	if err := UpsertMessage(conn, first); err != nil {
		t.Fatal(err)
	}

	second := base
	second.ID = "later@example.com"
	second.From = canon.Person{ID: "alice@example.com", Name: "Alice Smith", Email: "alice@example.com"}
	second.SentAt = time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC)
	if err := UpsertMessage(conn, second); err != nil {
		t.Fatal(err)
	}

	if got := graphCount(t, conn, `MATCH (p:Person {id:'alice@example.com'}) RETURN count(p)`); got != 1 {
		t.Fatalf("alice person duplicated (count %d)", got)
	}
	res, err := conn.Query(`MATCH (p:Person {id:'alice@example.com'}) RETURN p.name`)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	if !res.HasNext() {
		t.Fatal("alice node not found")
	}
	row, err := res.Next()
	if err != nil {
		t.Fatal(err)
	}
	vals, _ := row.GetAsSlice()
	if len(vals) < 1 || fmt.Sprint(vals[0]) != "Alice Smith" {
		t.Fatalf("alice name = %v, want 'Alice Smith' (last write wins)", vals)
	}
}

// Граф-таблицы сосуществуют со старыми Leaf/Person: лист пишется и читается
// после создания Message-схемы (имитация «105k leafs не ломаются» на малом
// масштабе).
func TestMessageGraphCoexistsWithLeaf(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	if _, err := UpsertLeaf(conn, LeafInput{
		Text: "existing leaf unique-graph-token", Source: "graph-test.md",
	}); err != nil {
		t.Fatal(err)
	}
	msgs := mailPairFixture()
	if err := UpsertMessages(conn, msgs); err != nil {
		t.Fatal(err)
	}
	if got := graphCount(t, conn, `MATCH (l:Leaf) RETURN count(l)`); got != 1 {
		t.Fatalf("Leaf count = %d, want 1 (Leaf data must survive Message schema)", got)
	}
	if got := graphCount(t, conn, `MATCH (m:Message) RETURN count(m)`); got != 2 {
		t.Fatalf("Message count = %d, want 2", got)
	}
	// FTS-индекс по-прежнему ставится после граф-записи (EnsureIndexes не сломан)
	if err := EnsureIndexes(conn); err != nil {
		t.Fatal(err)
	}
}
