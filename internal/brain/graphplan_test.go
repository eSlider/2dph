package brain

// Юнит-тесты D-1.2 (#259) на чистой логике (cgo-free): план записи
// canon.Message → Person/Message узлы + рёбра SENT/TO/CC/BCC/REPLY_TO.
// Реальная Ladybug не нужна — проверяем деривацию persons/edges и генерацию
// MERGE-запросов; идемпотентность на живой БД — graph_test.go (cgo).

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/canon"
)

// graphMsgFixture — synthetic mail (Alice/Bob/example.com, без PII), покрывает
// весь словарь рёбер D-1.2 кроме PART_OF (на этом этапе не материализуется).
func graphMsgFixture() MessageInput {
	reply := "parent@example.com"
	return MessageInput{
		Message: canon.Message{
			ID:       "child@example.com",
			ThreadID: "thread-1",
			Platform: "mail",
			From:     canon.Person{ID: "alice@example.com", Name: "Alice", Email: "alice@example.com"},
			ReplyTo:  &reply,
			To:       []canon.Person{{ID: "bob@example.com", Name: "Bob", Email: "bob@example.com"}},
			CC:       []canon.Person{{ID: "carol@example.com", Name: "Carol", Email: "carol@example.com"}},
			BCC:      []canon.Person{{ID: "dave@example.com", Email: "dave@example.com"}},
			SentAt:   time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC),
			Body:     "first paragraph\n\nsecond paragraph",
		},
		Folder:   "INBOX",
		Subject:  "Re: thread",
		GatorRef: "kind=mail#v-abc12345",
	}
}

// Persons уникальны по ID, порядок детерминированный: From → To → CC → BCC,
// дубликат в пределах письма не плодит узел (name первого вхождения).
func TestPlanGraphPersonsUniqueOrdered(t *testing.T) {
	m := graphMsgFixture()
	// alice дублируется в To с другим name — узел должен остаться один,
	// name из From (первое вхождение).
	m.To = append(m.To, canon.Person{ID: "alice@example.com", Name: "Alice (To)", Email: "alice@example.com"})

	persons, edges := planGraph(m)
	if len(persons) != 4 {
		t.Fatalf("persons = %d, want 4 (alice/bob/carol/dave): %+v", len(persons), persons)
	}
	want := []struct{ id, name string }{
		{"alice@example.com", "Alice"},
		{"bob@example.com", "Bob"},
		{"carol@example.com", "Carol"},
		{"dave@example.com", ""},
	}
	for i, w := range want {
		if persons[i].ID != w.id || persons[i].Name != w.name {
			t.Fatalf("persons[%d] = %+v, want id=%s name=%q", i, persons[i], w.id, w.name)
		}
	}
	// alice дублируется в To → canon.Edges даёт и SENT, и отдельный TO на неё:
	// рёбра ролевые (per-recipient), дедуп узлов на рёбра не влияет.
	if len(edges) != 6 {
		t.Fatalf("edges = %d, want 6 (SENT+TOx2+CC+BCC+REPLY_TO), got %+v", len(edges), edges)
	}
}

// Person без ID (нет email/handle) узлом не становится и рёбер не даёт.
func TestPlanGraphSkipsIDlessPerson(t *testing.T) {
	m := graphMsgFixture()
	m.From = canon.Person{} // Edges() уже не даёт SENT
	persons, edges := planGraph(m)
	for _, p := range persons {
		if p.ID == "" {
			t.Fatalf("id-less person in plan: %+v", p)
		}
	}
	for _, e := range edges {
		if e.Type == canon.EdgeSENT {
			t.Fatal("SENT edge must not be planned without a From person")
		}
	}
	if len(edges) != 4 {
		t.Fatalf("edges = %d, want 4 (no SENT)", len(edges))
	}
}

// Рёбра = canon.Edges() минус PART_OF (этап D-1.2 — параграфы не пишутся) и
// минус REPLY_TO на самого себя. Направление сохраняется as-is из #99.
func TestPlanGraphEdgesDropPartOfAndSelfReply(t *testing.T) {
	m := graphMsgFixture()
	// само-REPLY_TO canon.Edges() даст, но план обязан отбросить.
	self := m.ID
	m.ReplyTo = &self
	m.Body = "single paragraph" // чтобы PART_OF был ровно 1

	canonEdges := m.Edges()
	hasPartOf := false
	for _, e := range canonEdges {
		if e.Type == canon.EdgePartOf {
			hasPartOf = true
		}
	}
	if !hasPartOf {
		t.Fatal("fixture must produce PART_OF edges (canon.Edges contract)")
	}

	_, edges := planGraph(m)
	for _, e := range edges {
		if e.Type == canon.EdgePartOf {
			t.Fatalf("PART_OF must be dropped on this stage, got %+v", e)
		}
		if e.Type == canon.EdgeReplyTo && e.From == e.To {
			t.Fatalf("self REPLY_TO must be dropped, got %+v", e)
		}
	}
	if len(edges) != 4 {
		t.Fatalf("edges = %d, want 4 (SENT/TO/CC/BCC, REPLY_TO dropped)", len(edges))
	}
}

// Один и тот же вход → один и тот же план (детерминизм = база идемпотентности
// MERGE по id).
func TestPlanGraphDeterministic(t *testing.T) {
	p1, e1 := planGraph(graphMsgFixture())
	p2, e2 := planGraph(graphMsgFixture())
	if !reflect.DeepEqual(p1, p2) {
		t.Fatalf("persons not deterministic:\n%+v\n%+v", p1, p2)
	}
	if !reflect.DeepEqual(e1, e2) {
		t.Fatalf("edges not deterministic:\n%+v\n%+v", e1, e2)
	}
}

// edgeSchema: каждый тип ребра D-1.2 даёт rel-имя и label-ы концов в
// направлении Ladybug-схемы; PART_OF на этом этапе не пишется (ok=false).
func TestEdgeSchema(t *testing.T) {
	want := map[canon.EdgeType][3]string{
		canon.EdgeSENT:    {"SENT", "Person", "Message"}, // (:Person)-[:SENT]->(:Message)
		canon.EdgeTO:      {"TO", "Message", "Person"},   // (:Message)-[:TO]->(:Person)
		canon.EdgeCC:      {"CC", "Message", "Person"},
		canon.EdgeBCC:     {"BCC", "Message", "Person"},
		canon.EdgeReplyTo: {"REPLY_TO", "Message", "Message"},
	}
	for et, w := range want {
		es, ok := edgeSchema(et)
		if !ok {
			t.Fatalf("edgeSchema(%s) = not ok", et)
		}
		got := [3]string{es.rel, es.fromLabel, es.toLabel}
		if got != w {
			t.Fatalf("edgeSchema(%s) = %v, want %v", et, got, w)
		}
	}
	if _, ok := edgeSchema(canon.EdgePartOf); ok {
		t.Fatal("edgeSchema(PART_OF) must report not ok on this stage")
	}
}

// Генерация MERGE-запросов: person/message — MERGE по id с SET свойств;
// ребро — MATCH обоих концов + MERGE rel (создаёт ребро только если его ещё
// нет — идемпотентность на уровне запроса).
func TestUpsertQueryShapes(t *testing.T) {
	pq := personUpsertQuery()
	for _, frag := range []string{"MERGE (p:Person {id:$id})", "SET p.name=$name", "p.email=$email"} {
		if !strings.Contains(pq, frag) {
			t.Fatalf("personUpsertQuery missing %q:\n%s", frag, pq)
		}
	}
	mq := messageUpsertQuery()
	for _, frag := range []string{
		"MERGE (m:Message {id:$id})",
		"m.thread_id=$tid", "m.folder=$folder", "m.subject=$subject",
		"m.sent_at=$sent", "m.gator_ref=$gref", "m.body=$body",
	} {
		if !strings.Contains(mq, frag) {
			t.Fatalf("messageUpsertQuery missing %q:\n%s", frag, mq)
		}
	}
	eq := edgeUpsertQuery(canon.EdgeTO)
	for _, frag := range []string{
		"MATCH (a:Message {id:$from}), (b:Person {id:$to})",
		"MERGE (a)-[:TO]->(b)",
	} {
		if !strings.Contains(eq, frag) {
			t.Fatalf("edgeUpsertQuery(TO) missing %q:\n%s", frag, eq)
		}
	}
	if !strings.Contains(edgeUpsertQuery(canon.EdgeSENT), "MERGE (a)-[:SENT]->(b)") {
		t.Fatal("edgeUpsertQuery(SENT) wrong direction")
	}
	if !strings.Contains(edgeUpsertQuery(canon.EdgeReplyTo), "MERGE (a)-[:REPLY_TO]->(b)") {
		t.Fatal("edgeUpsertQuery(REPLY_TO) wrong direction")
	}
}

// sentAtText форматирует SentAt в строку узла (RFC3339 UTC); zero → "".
func TestSentAtText(t *testing.T) {
	ts := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	if got := sentAtText(ts); got != "2026-08-23T10:00:00Z" {
		t.Fatalf("sentAtText = %q", got)
	}
	if got := sentAtText(time.Time{}); got != "" {
		t.Fatalf("zero SentAt must be empty, got %q", got)
	}
}
