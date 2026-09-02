package brain

// План записи сообщения в Ladybug-граф (D-1.2, #259) — чистая логика без
// liblbug: деривация Person-узлов и рёбер из canon.Message и генерация
// идемпотентных MERGE-запросов. Исполнение на живой БД — graph.go (cgo).
//
// Схема (InitSchema, аддитивно к Leaf/Person):
//
//	(:Person)-[:SENT]->(:Message)      отправитель
//	(:Message)-[:TO|CC|BCC]->(:Person) получатели
//	(:Message)-[:REPLY_TO]->(:Message) родитель по in_reply_to
//
// PART_OF (Message→Paragraph) из canon.Edges() на этом этапе не пишется
// (решение D-1.1: параграфы — позже, при запросе семантики).

import (
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/canon"
)

// MessageInput — canon.Message + атрибуты gator-узла, которых нет в canon #99
// (folder/subject из row gator kind=mail и gator_ref deeplink
// `kind=mail#v-<hash8>`). canon.Message встраивается as-is: рёбра берутся из
// его Edges(), ID/адреса — из полей.
type MessageInput struct {
	canon.Message
	Folder   string
	Subject  string
	GatorRef string
}

// personUpsertQuery — MERGE Person по id (email), name-мердж: повторный sync
// с тем же email перезаписывает name последним значением.
func personUpsertQuery() string {
	return `MERGE (p:Person {id:$id}) SET p.name=$name, p.email=$email`
}

// messageUpsertQuery — MERGE Message по id (= message_id).
func messageUpsertQuery() string {
	return `MERGE (m:Message {id:$id})
SET m.thread_id=$tid, m.folder=$folder, m.subject=$subject,
    m.sent_at=$sent, m.gator_ref=$gref, m.body=$body`
}

// edgeSchema описывает концы ребра в Ladybug-схеме. PART_OF не пишется на
// этапе D-1.2 — ok=false.
type edgeSchemaDesc struct {
	rel       string
	fromLabel string
	toLabel   string
}

func edgeSchema(t canon.EdgeType) (edgeSchemaDesc, bool) {
	switch t {
	case canon.EdgeSENT:
		return edgeSchemaDesc{"SENT", "Person", "Message"}, true
	case canon.EdgeTO:
		return edgeSchemaDesc{"TO", "Message", "Person"}, true
	case canon.EdgeCC:
		return edgeSchemaDesc{"CC", "Message", "Person"}, true
	case canon.EdgeBCC:
		return edgeSchemaDesc{"BCC", "Message", "Person"}, true
	case canon.EdgeReplyTo:
		return edgeSchemaDesc{"REPLY_TO", "Message", "Message"}, true
	default:
		return edgeSchemaDesc{}, false
	}
}

// edgeUpsertQuery — MATCH обоих концов + MERGE ребра: ребро создаётся только
// если его ещё нет (идемпотентность на уровне запроса). label-ы из схемы.
func edgeUpsertQuery(t canon.EdgeType) string {
	es, ok := edgeSchema(t)
	if !ok {
		return ""
	}
	return "MATCH (a:" + es.fromLabel + " {id:$from}), (b:" + es.toLabel + " {id:$to})" +
		" MERGE (a)-[:" + es.rel + "]->(b)"
}

// sentAtText форматирует SentAt узла в RFC3339 UTC; zero → "" (не заполняем).
func sentAtText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// planGraph деривирует из одного сообщения детерминированный план записи:
// уникальные Person-узлы (порядок From → To → CC → BCC, name первого
// вхождения) и рёбра canon.Edges() без PART_OF и само-REPLY_TO.
func planGraph(in MessageInput) (persons []canon.Person, edges []canon.Edge) {
	seen := map[string]bool{}
	add := func(p canon.Person) {
		if p.ID == "" || seen[p.ID] {
			return
		}
		seen[p.ID] = true
		persons = append(persons, p)
	}
	add(in.From)
	for _, p := range in.To {
		add(p)
	}
	for _, p := range in.CC {
		add(p)
	}
	for _, p := range in.BCC {
		add(p)
	}
	for _, e := range in.Edges() {
		if e.Type == canon.EdgePartOf {
			continue // Paragraph-узлы не материализуются (D-1.1)
		}
		if e.Type == canon.EdgeReplyTo && e.From == e.To {
			continue // REPLY_TO на себя не несёт информации
		}
		edges = append(edges, e)
	}
	return persons, edges
}

// trimField обрезает пробелы строкового свойства узла (folder/gator_ref из
// parquet могут нести stray whitespace). Пустое остаётся пустым.
func trimField(s string) string {
	return strings.TrimSpace(s)
}
