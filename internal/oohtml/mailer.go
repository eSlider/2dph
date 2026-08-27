package oohtml

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eslider/go-onlyoffice"
)

// Store is the minimal OnlyOffice Mail surface the render-check gate needs.
// Implemented by onlyoffice.Client in production; offline fakes in tests.
type Store interface {
	SaveMailDraft(ctx context.Context, p onlyoffice.SaveMailDraftParams) (map[string]any, error)
	GetMailMessage(ctx context.Context, messageID string) (map[string]any, error)
	SendMail(ctx context.Context, p onlyoffice.SendMailParams) (json.RawMessage, error)
}

// SendParams describes one branded mail to gate before sending.
type SendParams struct {
	From     string
	To       string
	Cc       string
	Bcc      string
	Subject  string
	Template TemplateData
}

// Mailer renders the branded HTML, saves it as a draft, fetches it back and
// only sends once the render check passes (issue #76: draft → save → get →
// assert → send; a failed check blocks send).
type Mailer struct {
	store Store
}

// NewMailer wires a Store into a gated mailer.
func NewMailer(s Store) *Mailer { return &Mailer{store: s} }

// Send runs the TDD gate: Build → SaveMailDraft → GetMailMessage back →
// RenderCheck → SendMail only when the check is clean. Returns the raw send
// response (json.RawMessage as returned by SendMail).
func (m *Mailer) Send(ctx context.Context, p SendParams) (json.RawMessage, error) {
	if strings.TrimSpace(p.To) == "" {
		return nil, fmt.Errorf("oohtml: Send: to is required")
	}
	if strings.TrimSpace(p.Subject) == "" {
		return nil, fmt.Errorf("oohtml: Send: subject is required")
	}

	body, err := Build(p.Template)
	if err != nil {
		return nil, fmt.Errorf("oohtml: Build: %w", err)
	}

	saved, err := m.store.SaveMailDraft(ctx, onlyoffice.SaveMailDraftParams{
		From: p.From, To: p.To, Cc: p.Cc, Bcc: p.Bcc, Subject: p.Subject, Body: body,
	})
	if err != nil {
		return nil, fmt.Errorf("oohtml: SaveMailDraft: %w", err)
	}
	id := onlyoffice.Int64FromMap(saved, "id")
	if id <= 0 {
		return nil, fmt.Errorf("oohtml: SaveMailDraft: no message id in response %v", saved)
	}

	back, err := m.store.GetMailMessage(ctx, fmt.Sprintf("%d", id))
	if err != nil {
		return nil, fmt.Errorf("oohtml: GetMailMessage: %w", err)
	}
	fetched := stringField(back, "htmlBody")
	if issues := RenderCheck(fetched, p.Template); len(issues) > 0 {
		return nil, fmt.Errorf("oohtml: render check failed, send blocked: %v", issues)
	}

	return m.store.SendMail(ctx, onlyoffice.SendMailParams{
		ID: id, From: p.From, To: p.To, Cc: p.Cc, Bcc: p.Bcc, Subject: p.Subject, Body: fetched,
	})
}

// stringField coerces a message field to a string (typed boundary: the
// OnlyOffice API returns strings for htmlBody, but maps are untyped).
func stringField(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	default:
		return fmt.Sprint(m[key])
	}
}
