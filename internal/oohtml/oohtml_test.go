package oohtml

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/emersion/go-message"
	"github.com/eslider/go-onlyoffice"
)

// sampleTemplate returns a realistic branded mail template for synthetic test
// data (Alice/example.com — no PII).
func sampleTemplate() TemplateData {
	return TemplateData{
		Greeting:     "Hello Alice,",
		BodyHTML:     "<p>We prepared a special offer for you.</p>\n<p>Open it before Friday.</p>",
		OfferHeading: "Limited-time offer",
		OfferText:    "20% off your next project.",
		CTAURL:       "https://example.com/offer",
		CTALabel:     "Claim the offer",
		SignerName:   "A. Oblivantsev",
		SignerRole:   "Founder",
		SiteURL:      "https://produktor.io",
		SiteLabel:    "produktor.io",
	}
}

// TestRenderCheckPassOnBuilt proves the golden path: a template built from
// valid data passes the render check with no issues.
func TestRenderCheckPassOnBuilt(t *testing.T) {
	html, err := Build(sampleTemplate())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if issues := RenderCheck(html, sampleTemplate()); len(issues) != 0 {
		t.Fatalf("expected clean render check, got: %v", issues)
	}
}

// TestRenderCheckMissingLogo asserts the cid logo reference is required.
func TestRenderCheckMissingLogo(t *testing.T) {
	html, err := Build(sampleTemplate())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	html = strings.ReplaceAll(html, "cid:"+LogoCID, "https://example.com/logo.gif")
	if !hasIssue(RenderCheck(html, sampleTemplate()), IssueMissingLogo) {
		t.Fatalf("expected missing-logo issue when cid reference is replaced by remote URL")
	}
}

// TestRenderCheckMissingGreeting asserts the greeting paragraph is required.
func TestRenderCheckMissingGreeting(t *testing.T) {
	html, err := Build(sampleTemplate())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	html = strings.ReplaceAll(html, "Hello Alice,", "Hi there,")
	if !hasIssue(RenderCheck(html, sampleTemplate()), IssueMissingGreeting) {
		t.Fatalf("expected missing-greeting issue")
	}
}

// TestRenderCheckMissingOffer asserts the offer block and CTA are required.
func TestRenderCheckMissingOffer(t *testing.T) {
	html, err := Build(sampleTemplate())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	html = strings.ReplaceAll(html, sampleTemplate().OfferHeading, "")
	html = strings.ReplaceAll(html, sampleTemplate().CTALabel, "")
	if !hasIssue(RenderCheck(html, sampleTemplate()), IssueMissingOffer) {
		t.Fatalf("expected missing-offer issue")
	}
}

// TestRenderCheckMissingSignature asserts the signature block is required.
func TestRenderCheckMissingSignature(t *testing.T) {
	html, err := Build(sampleTemplate())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	html = strings.ReplaceAll(html, sampleTemplate().SignerName, "")
	if !hasIssue(RenderCheck(html, sampleTemplate()), IssueMissingSignature) {
		t.Fatalf("expected missing-signature issue")
	}
}

// TestRenderCheckDuplicateChatline asserts no text block appears twice (the
// wave-1 bug: a chat/signature line duplicated in the rendered mail).
func TestRenderCheckDuplicateChatline(t *testing.T) {
	html, err := Build(sampleTemplate())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// Duplicate the first body paragraph to simulate a doubled line.
	dup := "<p>We prepared a special offer for you.</p>"
	html = html + dup
	if !hasIssue(RenderCheck(html, sampleTemplate()), IssueDuplicateChatline) {
		t.Fatalf("expected duplicate-chatline issue")
	}
}

func hasIssue(issues []CheckIssue, code Code) bool {
	for _, i := range issues {
		if i.Code == code {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// gated send flow (offline via an in-memory fake store)

type fakeStore struct {
	drafts   map[int64]string // id → html body
	nextID   int64
	sent     []onlyoffice.SendMailParams
	stripCID bool // simulate OnlyOffice dropping unknown cid refs on save
}

func newFakeStore() *fakeStore { return &fakeStore{drafts: map[int64]string{}} }

func (f *fakeStore) SaveMailDraft(_ context.Context, p onlyoffice.SaveMailDraftParams) (map[string]any, error) {
	f.nextID++
	body := p.Body
	if f.stripCID {
		body = strings.ReplaceAll(body, "cid:"+LogoCID, "cid:gone.gif")
	}
	f.drafts[f.nextID] = body
	return map[string]any{"id": f.nextID}, nil
}

func (f *fakeStore) GetMailMessage(_ context.Context, id string) (map[string]any, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, err
	}
	body, ok := f.drafts[n]
	if !ok {
		return nil, fmt.Errorf("fake store: no draft %d", n)
	}
	return map[string]any{"id": n, "htmlBody": body}, nil
}

func (f *fakeStore) SendMail(_ context.Context, p onlyoffice.SendMailParams) (json.RawMessage, error) {
	f.sent = append(f.sent, p)
	return json.RawMessage(`{"ok":true}`), nil
}

// TestMailerSendBlockedByRenderCheck proves send is blocked when the saved
// draft fails the render check (e.g. OnlyOffice strips the unknown logo cid on
// save — the exact wave-1 class of bug the gate exists for).
func TestMailerSendBlockedByRenderCheck(t *testing.T) {
	f := newFakeStore()
	f.stripCID = true
	m := NewMailer(f)
	_, err := m.Send(context.Background(), SendParams{To: "bob@example.com", Subject: "Offer", Template: sampleTemplate()})
	if err == nil || !strings.Contains(err.Error(), "render check") {
		t.Fatalf("expected render-check block, got err=%v", err)
	}
	if len(f.sent) != 0 {
		t.Fatalf("send must not be called when render check fails; sent=%d", len(f.sent))
	}
}

// TestMailerSendPassesGuard proves a clean render-check lets send through once.
func TestMailerSendPassesGuard(t *testing.T) {
	f := newFakeStore()
	m := NewMailer(f)
	out, err := m.Send(context.Background(), SendParams{To: "bob@example.com", Subject: "Offer", Template: sampleTemplate()})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("expected exactly one send, got %d", len(f.sent))
	}
	if f.sent[0].Subject != "Offer" || f.sent[0].To != "bob@example.com" {
		t.Fatalf("unexpected send params: %+v", f.sent[0])
	}
	if !strings.Contains(string(out), "ok") {
		t.Fatalf("unexpected result: %s", out)
	}
}

// TestMailerUsesPlainTextConversion proves plain-text input is converted to
// HTML paragraphs (via go-onlyoffice PlainTextToMailHTML) before the branded
// shell is applied and the render check passes.
func TestMailerUsesPlainTextConversion(t *testing.T) {
	d := sampleTemplate()
	d.BodyHTML = onlyoffice.PlainTextToMailHTML("First line.\n\nSecond line.")
	html, err := Build(d)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.Contains(html, "<p>First line.</p>") || !strings.Contains(html, "<p>Second line.</p>") {
		t.Fatalf("plain-text paragraphs not converted: %s", html)
	}
	if issues := RenderCheck(html, d); len(issues) != 0 {
		t.Fatalf("render check after plain-text conversion failed: %v", issues)
	}
}

// TestBuildMessageCID proves the cid-embedding: the built MIME message carries
// an inline image/gif part whose Content-ID matches the <img src="cid:...">
// reference in the HTML, so the logo renders without a remote fetch.
func TestBuildMessageCID(t *testing.T) {
	var buf bytes.Buffer
	err := BuildMessage(&buf, SendParams{
		From: "alice@example.com", To: "bob@example.com", Subject: "Offer",
	}, mustBuild(t), []byte("GIF89a test logo"))
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}

	entity, err := message.Read(&buf)
	if err != nil {
		t.Fatalf("message.Read: %v", err)
	}
	htmlHasCID := false
	logoPart := 0
	err = entity.Walk(func(_ []int, e *message.Entity, _ error) error {
		if ct, _, _ := e.Header.ContentType(); strings.HasPrefix(ct, "text/html") {
			b, _ := io.ReadAll(e.Body)
			htmlHasCID = strings.Contains(string(b), "cid:"+LogoCID)
		}
		cid := e.Header.Get("Content-ID")
		if strings.Trim(cid, "<>") == LogoCID {
			logoPart++
			disp, _, _ := e.Header.ContentDisposition()
			if disp != "inline" {
				t.Fatalf("logo part disposition=%q, want inline", disp)
			}
			if ct, _, _ := e.Header.ContentType(); !strings.HasPrefix(ct, "image/gif") {
				t.Fatalf("logo part content-type=%q, want image/gif", ct)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !htmlHasCID {
		t.Fatalf("html part does not reference cid:%s", LogoCID)
	}
	if logoPart != 1 {
		t.Fatalf("expected exactly one logo part with Content-ID %s, got %d", LogoCID, logoPart)
	}
}

func mustBuild(t *testing.T) string {
	t.Helper()
	html, err := Build(sampleTemplate())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return html
}
