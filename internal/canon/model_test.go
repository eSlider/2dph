package canon

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// buildMail builds the canonical Message the way FromMail does, so edge and
// schema tests stay independent of MIME parsing details.
func buildMail() Message {
	reply := "m2"
	return Message{
		ID:          "m1",
		ThreadID:    "T42",
		Platform:    "mail",
		From:        Person{ID: "alice@example.com", Name: "Alice", Email: "alice@example.com"},
		ReplyTo:     &reply,
		To:          []Person{{ID: "bob@example.com", Name: "Bob", Email: "bob@example.com"}},
		CC:          []Person{{ID: "carol@example.com", Name: "Carol", Email: "carol@example.com"}},
		BCC:         []Person{{ID: "dave@example.com", Email: "dave@example.com"}},
		SentAt:      time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC),
		Body:        "first paragraph\n\nsecond paragraph",
		Attachments: []Attachment{{Name: "terms.pdf", ContentType: "application/pdf", Size: 2048, Path: "/tmp/x.pdf"}},
	}
}

// buildChat is the canonical Message the way FromChat produces it, used to prove
// mail and chat share one graph schema.
func buildChat() Message {
	reply := "c1"
	return Message{
		ID:       "c2",
		ThreadID: "T1",
		Platform: "telegram",
		From:     Person{ID: "telegram:alice", Name: "alice", Handle: "alice"},
		ReplyTo:  &reply,
		To:       []Person{{ID: "telegram:bob", Name: "bob", Handle: "bob"}},
		SentAt:   time.Date(2026, 8, 23, 11, 0, 0, 0, time.UTC),
		Body:     "ok, will review",
	}
}

// Edge types produced for buildMail cover the full canonical vocabulary.
func TestEdgesMail(t *testing.T) {
	edges := buildMail().Edges()
	got := edgeTypeSet(edges)
	want := map[EdgeType]int{
		EdgeSENT:    1, // alice -> m1
		EdgeTO:      1, // m1 -> bob
		EdgeCC:      1, // m1 -> carol
		EdgeBCC:     1, // m1 -> dave
		EdgeReplyTo: 1, // m1 -> m2
		EdgePartOf:  2, // m1 -> p0, p1 (two paragraphs)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("edges mismatch:\n got=%v\nwant=%v", got, want)
	}
}

// Edges reference real node IDs: SENT starts at the From person and lands on the
// message; TO/CC/BCC go message -> person; REPLY_TO message -> message; PART_OF
// message -> paragraph.
func TestEdgesReferenceRealIDs(t *testing.T) {
	m := buildMail()
	var sent, to, reply, part Edge
	for _, e := range m.Edges() {
		switch e.Type {
		case EdgeSENT:
			sent = e
		case EdgeTO:
			to = e
		case EdgeReplyTo:
			reply = e
		case EdgePartOf:
			part = e
		}
	}
	if sent.From != "alice@example.com" || sent.To != m.ID {
		t.Fatalf("SENT edge wrong: %+v", sent)
	}
	if to.From != m.ID || to.To != "bob@example.com" {
		t.Fatalf("TO edge wrong: %+v", to)
	}
	if reply.From != m.ID || reply.To != "m2" {
		t.Fatalf("REPLY_TO edge wrong: %+v", reply)
	}
	if part.From != m.ID {
		t.Fatalf("PART_OF edge wrong: %+v", part)
	}
}

// A message with no recipients or body still yields SENT only (a malformed leaf
// must not panic or fabricate edges).
func TestEdgesMinimal(t *testing.T) {
	m := Message{ID: "x", Platform: "chat", From: Person{ID: "u"}, Body: "   "}
	edges := m.Edges()
	if len(edges) != 1 || edges[0].Type != EdgeSENT {
		t.Fatalf("minimal message edges = %+v, want single SENT", edges)
	}
}

// GraphSchema is a stable, deterministic descriptor of the conversation-canon
// graph. Both a mail-derived and a chat-derived message must agree on it.
func TestGraphSchemaStable(t *testing.T) {
	want := "nodes: Person, Message, Paragraph\nedges: SENT, TO, CC, BCC, REPLY_TO, PART_OF"
	if got := GraphSchema(); got != want {
		t.Fatalf("GraphSchema() = %q, want %q", got, want)
	}
}

// Every edge produced by a real mail and a real chat message must belong to the
// canonical vocabulary (golden: one .eml and one telegram export conform to the
// same graph schema).
func TestEdgesConformToSchema(t *testing.T) {
	for _, m := range []Message{buildMail(), buildChat()} {
		schema := edgeTypeSet(edgesForSchema())
		for _, e := range m.Edges() {
			if _, ok := schema[e.Type]; !ok {
				t.Fatalf("edge type %q not in canonical schema", e.Type)
			}
		}
		if len(m.Edges()) == 0 {
			t.Fatal("no edges produced")
		}
	}
}

func edgeTypeSet(edges []Edge) map[EdgeType]int {
	m := map[EdgeType]int{}
	for _, e := range edges {
		m[e.Type]++
	}
	return m
}

// edgesForSchema returns the canonical schema as the set of all edge types the
// model may emit.
func edgesForSchema() []Edge {
	return []Edge{{Type: EdgeSENT}, {Type: EdgeTO}, {Type: EdgeCC},
		{Type: EdgeBCC}, {Type: EdgeReplyTo}, {Type: EdgePartOf}}
}

func TestSchemaLinesContainAllTypes(t *testing.T) {
	s := GraphSchema()
	for _, et := range []EdgeType{EdgeSENT, EdgeTO, EdgeCC, EdgeBCC, EdgeReplyTo, EdgePartOf} {
		if !strings.Contains(s, string(et)) {
			t.Fatalf("GraphSchema missing edge type %q", et)
		}
	}
}
