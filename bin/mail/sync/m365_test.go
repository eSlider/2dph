package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// newM365TestClient serves the Graph delta + message endpoints against a fake
// token endpoint, so unit tests never touch the network.
func newM365TestClient(t *testing.T, graph http.Handler) *M365Client {
	t.Helper()
	tok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"test-token","expires_in":3600}`))
	}))
	gr := httptest.NewServer(graph)
	t.Cleanup(func() {
		tok.Close()
		gr.Close()
	})
	client, err := NewM365Client(M365Credentials{Tenant: "t.onmicrosoft.com", ClientID: "c", ClientSecret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	client.base = gr.URL
	client.tokenEndpoint = tok.URL
	return client
}

func TestM365AccessToken(t *testing.T) {
	c := newM365TestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	got, err := c.accessToken(context.Background())
	if err != nil {
		t.Fatalf("accessToken: %v", err)
	}
	if got != "test-token" {
		t.Errorf("token = %q", got)
	}
	// Second call must reuse the cached token (no token request).
	again, err := c.accessToken(context.Background())
	if err != nil || again != "test-token" {
		t.Fatalf("cached token: %q, %v", again, err)
	}
}

func TestM365DeltaSkipsTombstones(t *testing.T) {
	first := true
	graph := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if first {
			first = false
			w.Write([]byte(`{"value":[
				{"id":"m1"},
				{"id":"m2","@odata.removedReason":"deleted"},
				{"id":"m3"}
			],"@odata.deltaLink":"` + deltaNext + `"}`))
			return
		}
		// Second call must use the stored deltaLink (points at this server).
		if r.URL.Path != "/v1.0/delta-next" {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":{"message":"unexpected path"}}`))
			return
		}
		w.Write([]byte(`{"value":[{"id":"m4"}],"@odata.deltaLink":"` + deltaFinal + `"}`))
	})
	c := newM365TestClient(t, graph)
	deltaNext = c.base + "/v1.0/delta-next"
	deltaFinal = c.base + "/v1.0/delta-final"

	ids, link, err := c.ListDeltaIDs(context.Background(), "a@x.de", "", 0)
	if err != nil {
		t.Fatalf("delta: %v", err)
	}
	if len(ids) != 2 || ids[0] != "m1" || ids[1] != "m3" {
		t.Errorf("ids = %v", ids)
	}
	if link == "" {
		t.Error("expected new deltaLink")
	}
	// Incremental: pass the deltaLink, get only the new id.
	ids2, link2, err := c.ListDeltaIDs(context.Background(), "a@x.de", link, 0)
	if err != nil {
		t.Fatalf("delta incremental: %v", err)
	}
	if len(ids2) != 1 || ids2[0] != "m4" {
		t.Errorf("ids2 = %v", ids2)
	}
	if link2 == "" {
		t.Error("expected updated deltaLink")
	}
}

func TestM365GetMessageNormalizes(t *testing.T) {
	graph := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if pathLast(r.URL.Path) == "messages" {
			w.Write([]byte(`{"value":[{"id":"m1"}]}`))
			return
		}
		w.Write([]byte(`{
			"id":"m1",
			"subject":"Hallo",
			"from":{"emailAddress":{"name":"Max","address":"max@x.de"}},
			"toRecipients":[{"emailAddress":{"address":"a@x.de"}}],
			"receivedDateTime":"2026-08-14T08:15:00Z",
			"body":{"contentType":"html","content":"<p>body</p>"},
			"bodyPreview":"body",
			"internetMessageId":"<mid@x.de>",
			"attachments":[
				{"id":"att1","name":"doc.pdf","contentType":"application/pdf","size":10},
				{"id":"img1","name":"logo.png","contentType":"image/png","isInline":true}
			]
		}`))
	})
	c := newM365TestClient(t, graph)
	m, err := c.GetMessage(context.Background(), "a@x.de", "m1")
	if err != nil {
		t.Fatalf("GetMessage: %v", err)
	}
	if m.ID != "m1" || m.Subject != "Hallo" || m.From != "Max <max@x.de>" || m.To != "a@x.de" {
		t.Errorf("headers mismatch: %+v", m)
	}
	if m.HTMLBody != "<p>body</p>" {
		t.Errorf("html = %q", m.HTMLBody)
	}
	if m.ReceivedAt.IsZero() {
		t.Error("receivedAt zero")
	}
	if m.MimeMessageID != "<mid@x.de>" {
		t.Errorf("mime id = %q", m.MimeMessageID)
	}
	if len(m.Attachments) != 1 || m.Attachments[0].FileName != "doc.pdf" || m.Attachments[0].FileID != "att1" {
		t.Errorf("atts = %+v", m.Attachments)
	}
	if !m.HasAttachments {
		t.Error("expected hasAttachments")
	}
}

func TestM365SourceDeltaState(t *testing.T) {
	graph := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value":[{"id":"m1"}],"@odata.deltaLink":"` + deltaNext + `"}`))
	})
	c := newM365TestClient(t, graph)
	deltaNext = c.base + "/v1.0/delta-next"
	stateDir := filepath.Join(t.TempDir(), ".m365")
	s := &m365Source{c: c, mailbox: "info@x.de", localpart: "info", stateDir: stateDir}

	if s.Folder() != "m365/info" {
		t.Errorf("folder = %q", s.Folder())
	}
	ids, _, err := s.ListIDs(context.Background(), 0, "")
	if err != nil {
		t.Fatalf("ListIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "m1" {
		t.Errorf("ids = %v", ids)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(stateDir, "info.deltalink"))
	if err != nil {
		t.Fatalf("read delta state: %v", err)
	}
	if string(data) != deltaNext {
		t.Errorf("delta state = %q, want %q", string(data), deltaNext)
	}
}

func pathLast(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

// deltaNext/deltaFinal are set per-test from the fake graph server URL so
// deltaLink values always point back at the fake (never the real Graph).
var deltaNext, deltaFinal string
