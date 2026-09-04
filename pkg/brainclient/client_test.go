package brainclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/eSlider/2dph/internal/contract"
)

// Клиент read-контракта (P-9.5): тонкая HTTP-обёртка над search/get/stats/
// audit с typed-ответами internal/contract. Тесты офлайн против
// httptest-фикстур — без сети и без kb.lbug.

const (
	cSearch = `{"contract_version":"1.0","query":"matrix federation","root_filter":"facts","as_of":"2026-01-01","count":1,"results":[{"id":"abc123","text":"leaf","root":"facts","confidence":"confirmed","score":0.9}]}`
	cGet    = `{"contract_version":"1.0","id":"abc123","root":"facts","confidence":"confirmed","source":"docs","type":"fact","text":"full body"}`
	cStats  = `{"contract_version":"1.0","total":105220,"by_root":{"facts":21,"info":105199},"db":"/var/kb.lbug"}`
	cAudit  = `{"contract_version":"1.0","status":"ok","by_confidence":[{"root":"facts","confidence":"confirmed","count":21}]}`
)

// newFixtureServer отвечает контрактными фикстурами; last() возвращает
// последний запрос (метод, путь, query, заголовки) для проверок клиента.
func newFixtureServer(t *testing.T) (*httptest.Server, func() *http.Request) {
	t.Helper()
	var mu sync.Mutex
	var last *http.Request
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		last = r.Clone(r.Context())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(cSearch))
		case "/get":
			w.Write([]byte(cGet))
		case "/stats":
			w.Write([]byte(cStats))
		case "/audit":
			w.Write([]byte(cAudit))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts, func() *http.Request {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

func TestSearchTyped(t *testing.T) {
	ts, last := newFixtureServer(t)
	c := New(Config{Base: ts.URL})

	resp, err := c.Search(context.Background(), "matrix federation", SearchOptions{
		Root: "facts", AsOf: "2026-01-01", Limit: 5,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.ContractVersion != contract.ReadContractVersion || resp.Count != 1 {
		t.Fatalf("typed response mismatch: %+v", resp)
	}
	if len(resp.Results) != 1 || resp.Results[0].ID != "abc123" {
		t.Fatalf("results: %+v", resp.Results)
	}
	req := last()
	if req.URL.Path != "/search" {
		t.Fatalf("path = %q, want /search", req.URL.Path)
	}
	q := req.URL.Query()
	if q.Get("q") != "matrix federation" || q.Get("root") != "facts" ||
		q.Get("as_of") != "2026-01-01" || q.Get("n") != "5" {
		t.Fatalf("query params: %v", q)
	}
}

func TestSearchOptionsValidation(t *testing.T) {
	// Ошибки валидации опций не делают сетевых вызовов.
	c := New(Config{Base: "http://127.0.0.1:1"}) // недоступный base — вызовов быть не должно
	cases := []struct {
		name string
		opt  SearchOptions
	}{
		{"bad root", SearchOptions{Root: "other"}},
		{"bad as_of", SearchOptions{AsOf: "01-01-2025"}},
		{"limit too big", SearchOptions{Limit: 101}},
		{"limit negative", SearchOptions{Limit: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Search(context.Background(), "q", tc.opt); err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
	if _, err := c.Search(context.Background(), "", SearchOptions{}); err == nil {
		t.Fatal("empty query must fail client-side")
	}
}

func TestSearchRejectsContractViolation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"contract_version":"1.0","query":"q","root_filter":"","count":1,"results":[{"id":"abc","text":"t","score":0.1}]}`))
	}))
	defer ts.Close()
	c := New(Config{Base: ts.URL})
	if _, err := c.Search(context.Background(), "q", SearchOptions{}); err == nil {
		t.Fatal("format violation (hit without root) must fail the client")
	}
}

func TestSearchMissingContractVersion(t *testing.T) {
	// Сервис до read-контракта: typed-клиент строг и отказывает.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"query":"q","root_filter":"","count":1,"results":[]}`))
	}))
	defer ts.Close()
	c := New(Config{Base: ts.URL})
	if _, err := c.Search(context.Background(), "q", SearchOptions{}); err == nil {
		t.Fatal("missing contract_version must fail the typed client")
	}
}

func TestGetBodyVariants(t *testing.T) {
	ts, _ := newFixtureServer(t)
	c := New(Config{Base: ts.URL})

	got, err := c.Get(context.Background(), "abc123", GetOptions{Body: true})
	if err != nil {
		t.Fatalf("Get(body=true): %v", err)
	}
	if got.Text != "full body" || got.Root != "facts" || got.Source != "docs" {
		t.Fatalf("get response: %+v", got)
	}
}

func TestStatsAndAudit(t *testing.T) {
	ts, _ := newFixtureServer(t)
	c := New(Config{Base: ts.URL})

	st, err := c.Stats(context.Background())
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Total != 105220 || st.ByRoot["facts"] != 21 {
		t.Fatalf("stats: %+v", st)
	}

	au, err := c.Audit(context.Background())
	if err != nil {
		t.Fatalf("Audit: %v", err)
	}
	if len(au.ByConfidence) != 1 || au.ByConfidence[0].Confidence != "confirmed" {
		t.Fatalf("audit: %+v", au.ByConfidence)
	}
}

func TestHTTPErrorSurfaces(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"no leaf x"}`, http.StatusNotFound)
	}))
	defer ts.Close()
	c := New(Config{Base: ts.URL})
	_, err := c.Get(context.Background(), "x", GetOptions{})
	if err == nil {
		t.Fatal("HTTP 404 must surface as an error")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "no leaf x") {
		t.Fatalf("error must carry status and body, got: %v", err)
	}
}

func TestAuthHeader(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sekret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(cSearch))
	}))
	defer ts.Close()
	c := New(Config{Base: ts.URL, Token: "sekret"})
	if _, err := c.Search(context.Background(), "q", SearchOptions{}); err != nil {
		t.Fatalf("authed search failed: %v", err)
	}
}

func TestFactsGatedEndToEnd(t *testing.T) {
	// Сервер отвечает смесью: confirmed-факт, гипотеза (2v2), partial, info.
	mixed := `{"contract_version":"1.0","query":"q","root_filter":"","count":4,"results":[
		{"id":"f-ok","text":"ok","root":"facts","confidence":"confirmed","score":0.9},
		{"id":"f-hyp","text":"a x b vs c x d","root":"facts","confidence":"hypothesis","score":0.8},
		{"id":"f-par","text":"p","root":"facts","confidence":"partial","score":0.7},
		{"id":"i-note","text":"n","root":"info","confidence":"confirmed","score":0.6}]}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("root") != "facts" {
			t.Errorf("Facts must force root=facts on the request, got %q", r.URL.Query().Get("root"))
		}
		w.Write([]byte(mixed))
	}))
	defer ts.Close()
	c := New(Config{Base: ts.URL})

	f, err := c.Facts(context.Background(), "q", SearchOptions{})
	if err != nil {
		t.Fatalf("Facts: %v", err)
	}
	if f.RootFilter != "facts" || f.Count != 1 {
		t.Fatalf("root_filter/count: %q / %d", f.RootFilter, f.Count)
	}
	if len(f.Confirmed) != 1 || f.Confirmed[0].ID != "f-ok" {
		t.Fatalf("confirmed: %+v", f.Confirmed)
	}
	if len(f.NotConfirmed) != 3 {
		t.Fatalf("not_confirmed = %d, want 3", len(f.NotConfirmed))
	}
	for _, nf := range f.NotConfirmed {
		if nf.ID == "f-hyp" && !strings.Contains(nf.Reason, "hypothesis") {
			t.Errorf("f-hyp reason must mention hypothesis: %q", nf.Reason)
		}
	}
	// Сырой JSON (клиентская схема): not_confirmed — аддитивное поле, каждый
	// отклонённый хит несёт reason.
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "not_confirmed") || !strings.Contains(string(raw), "reason") {
		t.Fatalf("json must carry not_confirmed with reasons: %s", raw)
	}
}
