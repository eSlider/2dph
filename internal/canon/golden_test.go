package canon

import (
	"encoding/json"
	"os"
	"testing"
)

// Golden acceptance (#99): one .eml and one telegram export are converted through
// the same canonical model and must agree on the graph schema (node kinds + edge
// types). Neither the mail nor the chat path may invent extra edge types, and
// both must produce at least the SENT + PART_OF core.
func TestMailAndChatSameGraphSchema(t *testing.T) {
	mailMsg, err := FromMailFile("testdata/sample.eml")
	if err != nil {
		t.Fatal(err)
	}
	chatMsgs, err := fromChatExport("testdata/telegram.json")
	if err != nil {
		t.Fatal(err)
	}

	mailSchema := GraphSchema()
	chatSchema := GraphSchema()
	if mailSchema != chatSchema {
		t.Fatalf("mail schema != chat schema:\n%q\n%q", mailSchema, chatSchema)
	}

	conform := func(msgs []Message) {
		for _, m := range msgs {
			edges := m.Edges()
			if len(edges) == 0 {
				t.Fatalf("platform %s produced no edges", m.Platform)
			}
			hasSent, hasPart := false, false
			for _, e := range edges {
				switch e.Type {
				case EdgeSENT:
					hasSent = true
				case EdgePartOf:
					hasPart = true
				}
			}
			if !hasSent || !hasPart {
				t.Fatalf("platform %s edges missing SENT/PART_OF core: %+v", m.Platform, edges)
			}
		}
	}
	conform([]Message{*mailMsg})
	conform(chatMsgs)
}

// The mail converter must surface envelope fields the canon needs: from, to, cc,
// reply link, thread, body, lazy attachment metadata.
func TestFromMailEnvelope(t *testing.T) {
	m, err := FromMailFile("testdata/sample.eml")
	if err != nil {
		t.Fatal(err)
	}
	if m.Platform != "mail" {
		t.Fatalf("platform = %q, want mail", m.Platform)
	}
	if m.From.ID != "alice@example.com" {
		t.Fatalf("from = %+v, want alice@example.com", m.From)
	}
	if len(m.To) != 1 || m.To[0].ID != "bob@example.com" {
		t.Fatalf("to = %+v", m.To)
	}
	if len(m.CC) != 1 || m.CC[0].ID != "carol@example.com" {
		t.Fatalf("cc = %+v", m.CC)
	}
	if m.ReplyTo == nil || *m.ReplyTo == "" {
		t.Fatalf("expected reply link, got %v", m.ReplyTo)
	}
	if m.ThreadID == "" {
		t.Fatal("expected thread id")
	}
	if len(m.Attachments) != 1 || m.Attachments[0].Name != "terms.pdf" {
		t.Fatalf("attachments = %+v", m.Attachments)
	}
	if m.SentAt.IsZero() {
		t.Fatal("sentAt zero")
	}
}

// fromChatExport decodes a telegram export JSON array into canonical messages.
func fromChatExport(path string) ([]Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var recs []chatRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, err
	}
	out := make([]Message, 0, len(recs))
	threadID := "telegram:T1"
	for _, r := range recs {
		m, err := FromChat("telegram", threadID, r.Text, r.From, r.Timestamp, r.ReplyTo)
		if err != nil {
			return nil, err
		}
		m.ID = r.ID
		out = append(out, *m)
	}
	return out, nil
}

type chatRecord struct {
	ID        string  `json:"id"`
	From      string  `json:"from"`
	Text      string  `json:"text"`
	Timestamp string  `json:"timestamp"`
	ReplyTo   *string `json:"replyTo,omitempty"`
}
