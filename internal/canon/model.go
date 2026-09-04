// Package canon is the conversation-canon evidence layer for the sync-ETL
// pipeline (epic #88, issue #99). One canonical Message model covers mail AND
// chat platforms; it is stored on disk as JSON under var/corpus/{mail,chats}
// with a sha256 manifest, and the brain ingests ONLY this canon.
//
// Graph edges are derived from the canon: (:Person)-[:SENT]->(:Message) and
// (:Message)-[:TO|CC|BCC]->(:Person), [:REPLY_TO] threads, [:PART_OF] body
// paragraphs. Any format may be converted into this canon; the graph schema is
// identical for mail and chat.
package canon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Person is one actor in a conversation. For mail the ID is the (lowercased)
// email address; for chat it is "platform:handle". Exactly one of Email/Handle
// is set depending on platform; the other stays empty.
type Person struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Email  string `json:"email,omitempty"`
	Handle string `json:"handle,omitempty"`
}

// Attachment is a lazy attachment reference: only metadata is materialized at
// canon time; the body is opened on demand via Open. Bodies are never buffered.
type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Path        string `json:"path,omitempty"`
}

// Open returns a reader over the attachment body, or an error when no path is
// recorded. It never buffers the whole body into memory.
func (a Attachment) Open(ctx context.Context) (io.ReadCloser, error) {
	if a.Path == "" {
		return nil, errors.New("canon: attachment has no path")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return os.Open(a.Path)
}

// Message is the single canonical model for a mail or chat message.
type Message struct {
	ID          string       `json:"id"`
	ThreadID    string       `json:"threadId"`
	Platform    string       `json:"platform"`
	From        Person       `json:"from"`
	ReplyTo     *string      `json:"replyTo,omitempty"` // ID this message replies to (REPLY_TO chain)
	To          []Person     `json:"to,omitempty"`
	CC          []Person     `json:"cc,omitempty"`
	BCC         []Person     `json:"bcc,omitempty"`
	SentAt      time.Time    `json:"sentAt"`
	Body        string       `json:"body,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// EdgeType is the canonical edge vocabulary of the conversation graph.
type EdgeType string

const (
	EdgeSENT    EdgeType = "SENT"
	EdgeTO      EdgeType = "TO"
	EdgeCC      EdgeType = "CC"
	EdgeBCC     EdgeType = "BCC"
	EdgeReplyTo EdgeType = "REPLY_TO"
	EdgePartOf  EdgeType = "PART_OF"
)

// Edge is one directed graph edge. From/To carry the stable node IDs defined by
// the canon (person id, message id, paragraph id).
type Edge struct {
	Type EdgeType `json:"type"`
	From string   `json:"from"`
	To   string   `json:"to"`
}

// GraphSchema returns a stable, deterministic descriptor of the conversation
// graph. Both a mail-derived and a chat-derived canon must report exactly this
// schema — one .eml and one telegram export map into the same graph.
func GraphSchema() string {
	return "nodes: Person, Message, Paragraph\nedges: SENT, TO, CC, BCC, REPLY_TO, PART_OF"
}

var reParagraph = regexp.MustCompile(`\n{2,}`)

// paragraphs splits Body into non-empty blocks separated by blank lines. Each
// block becomes a Paragraph node PART_OF the message.
func paragraphs(body string) []string {
	parts := reParagraph.Split(body, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// paragraphID derives a stable paragraph node id from the message id and the
// paragraph index.
func paragraphID(msgID string, idx int) string {
	sum := sha256.Sum256([]byte(msgID + "\x00" + strconv.Itoa(idx)))
	return hex.EncodeToString(sum[:8])
}

// Edges derives the canonical graph edges for the message:
//
//	(:Person)-[:SENT]->(:Message)
//	(:Message)-[:TO|CC|BCC]->(:Person)
//	(:Message)-[:REPLY_TO]->(:Message)
//	(:Message)-[:PART_OF]->(:Paragraph)
func (m Message) Edges() []Edge {
	edges := make([]Edge, 0, 1+len(m.To)+len(m.CC)+len(m.BCC)+len(paragraphs(m.Body))+1)
	if m.From.ID != "" {
		edges = append(edges, Edge{Type: EdgeSENT, From: m.From.ID, To: m.ID})
	}
	for _, p := range m.To {
		if p.ID != "" {
			edges = append(edges, Edge{Type: EdgeTO, From: m.ID, To: p.ID})
		}
	}
	for _, p := range m.CC {
		if p.ID != "" {
			edges = append(edges, Edge{Type: EdgeCC, From: m.ID, To: p.ID})
		}
	}
	for _, p := range m.BCC {
		if p.ID != "" {
			edges = append(edges, Edge{Type: EdgeBCC, From: m.ID, To: p.ID})
		}
	}
	if m.ReplyTo != nil && *m.ReplyTo != "" {
		edges = append(edges, Edge{Type: EdgeReplyTo, From: m.ID, To: *m.ReplyTo})
	}
	for i, p := range paragraphs(m.Body) {
		if p != "" {
			edges = append(edges, Edge{Type: EdgePartOf, From: m.ID, To: paragraphID(m.ID, i)})
		}
	}
	return edges
}
