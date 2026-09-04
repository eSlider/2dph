package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/config"
)

// fakeSearcher is an injectable Searcher for tests (no python involved).
type fakeSearcher struct {
	mu       sync.Mutex
	delay    time.Duration
	calls    int
	active   atomic.Int32
	maxSeen  atomic.Int32
	callback func(q string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error)
}

func (f *fakeSearcher) Search(ctx context.Context, query string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	n := f.active.Add(1)
	for {
		old := f.maxSeen.Load()
		if n <= old || f.maxSeen.CompareAndSwap(old, n) {
			break
		}
	}
	defer f.active.Add(-1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.callback != nil {
		return f.callback(query, limit, asOf, root, sort, noWeb)
	}
	return []byte(`{"query":"` + query + `","count":0,"results":[]}`), nil
}

func (f *fakeSearcher) Get(_ context.Context, id string, body bool) ([]byte, error) {
	out := map[string]any{"id": id, "root": "info"}
	if body {
		out["text"] = "fake body"
	}
	return json.Marshal(out)
}

func (f *fakeSearcher) Stats(context.Context) ([]byte, error) {
	return []byte(`{"total":0,"by_root":{}}`), nil
}

func (f *fakeSearcher) Audit(context.Context) ([]byte, error) {
	return []byte(`{"status":"ok"}`), nil
}

func (f *fakeSearcher) Ingest(_ context.Context, body []byte) ([]byte, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return []byte(`{"mode":"add","command":"bin/brain/add.go"}`), nil
	}
	return []byte(`{"mode":"add","ids":["fake-leaf"]}`), nil
}

func (f *fakeSearcher) Leafs(_ context.Context, root, typ, source, text string, limit int) ([]byte, error) {
	return json.Marshal(map[string]any{
		"root": root, "type": typ, "source": source, "text": text, "limit": limit,
		"leafs": []any{map[string]any{"id": "leaf-fake"}},
	})
}

func (f *fakeSearcher) Edges(_ context.Context, id string) ([]byte, error) {
	return json.Marshal(map[string]any{"id": id, "edges": []any{map[string]any{"to": "b", "type": "synapse"}}})
}

func (f *fakeSearcher) AddEdge(_ context.Context, body []byte) ([]byte, error) {
	var in map[string]any
	_ = json.Unmarshal(body, &in)
	return json.Marshal(map[string]any{"from": in["from"], "to": in["to"], "type": in["type"]})
}

func (f *fakeSearcher) Path(_ context.Context, from, to string, max int) ([]byte, error) {
	return json.Marshal(map[string]any{"from": from, "to": to, "max": max, "path": []string{from, to}})
}

func (f *fakeSearcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func get(t *testing.T, h http.Handler, path string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func TestHealth(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	code, body := get(t, h, "/health")
	if code != http.StatusOK {
		t.Fatalf("health code = %d, want 200", code)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("health body not json: %v (%s)", err, body)
	}
	if out["status"] != "ok" {
		t.Fatalf("health status = %v, want ok", out["status"])
	}
}

func TestSearchMissingQuery(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	if code, _ := get(t, h, "/search"); code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", code)
	}
}

func TestSearchReturnsSearcherResult(t *testing.T) {
	fs := &fakeSearcher{callback: func(q string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
		return []byte(`{"query":"` + q + `","count":1,"results":[{"id":"x"}]}`), nil
	}}
	h := NewServer(fs, 1)
	code, body := get(t, h, "/search?q=matrix")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("body not json: %v (%s)", err, body)
	}
	if out["query"] != "matrix" {
		t.Fatalf("query = %v, want matrix", out["query"])
	}
	if fs.count() != 1 {
		t.Fatalf("searcher called %d times, want 1", fs.count())
	}
}

func TestSearchPassesRootFilter(t *testing.T) {
	var gotRoot string
	fs := &fakeSearcher{callback: func(q string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
		gotRoot = root
		return []byte(`{"query":"` + q + `","count":0,"results":[]}`), nil
	}}
	h := NewServer(fs, 1)
	if code, _ := get(t, h, "/search?q=ops&root=facts"); code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if gotRoot != "facts" {
		t.Fatalf("root = %q, want facts", gotRoot)
	}
	if code, _ := get(t, h, "/search?q=ops"); code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if gotRoot != "" {
		t.Fatalf("root = %q, want empty when omitted", gotRoot)
	}
}

func TestSearchPassesNoweb(t *testing.T) {
	var gotNoWeb bool
	fs := &fakeSearcher{callback: func(q string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
		gotNoWeb = noWeb
		return []byte(`{"query":"` + q + `","count":0,"results":[]}`), nil
	}}
	h := NewServer(fs, 1)
	if code, _ := get(t, h, "/search?q=ops&noweb=1"); code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if !gotNoWeb {
		t.Fatalf("noweb = false, want true when ?noweb=1")
	}
	if code, _ := get(t, h, "/search?q=ops"); code != http.StatusOK {
		t.Fatalf("code = %d, want 200", code)
	}
	if gotNoWeb {
		t.Fatalf("noweb = true, want false when omitted")
	}
}

func TestMCPSearchPassesRootFilter(t *testing.T) {
	var gotRoot string
	fs := &fakeSearcher{callback: func(q string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
		gotRoot = root
		return []byte(`{"query":"` + q + `","count":0,"results":[]}`), nil
	}}
	h := NewServer(fs, 1)
	code, body := postJSON(t, h, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"q":"ops","root":"facts"}}}`)
	if code != http.StatusOK {
		t.Fatalf("call code = %d body=%s", code, body)
	}
	if gotRoot != "facts" {
		t.Fatalf("mcp root = %q, want facts", gotRoot)
	}
}

func TestMCPSearchPassesNoweb(t *testing.T) {
	var gotNoWeb bool
	fs := &fakeSearcher{callback: func(q string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
		gotNoWeb = noWeb
		return []byte(`{"query":"` + q + `","count":0,"results":[]}`), nil
	}}
	h := NewServer(fs, 1)
	code, body := postJSON(t, h, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"q":"ops","noweb":true}}}`)
	if code != http.StatusOK {
		t.Fatalf("call code = %d body=%s", code, body)
	}
	if !gotNoWeb {
		t.Fatalf("mcp noweb = false, want true")
	}
}

func TestSearchConcurrencyBounded(t *testing.T) {
	// 8 parallel requests on a 3-worker pool: at most 3 concurrent searches.
	fs := &fakeSearcher{delay: 20 * time.Millisecond}
	h := NewServer(fs, 3)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/search?q=abc", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("code = %d, want 200", rec.Code)
			}
		}()
	}
	wg.Wait()

	if calls := fs.count(); calls != 8 {
		t.Fatalf("searcher called %d times, want 8", calls)
	}
	if max := fs.maxSeen.Load(); max > 3 {
		t.Fatalf("max concurrent = %d, want <= 3", max)
	}
	if max := fs.maxSeen.Load(); max < 1 {
		t.Fatalf("max concurrent = %d, want >= 1", max)
	}
}

func TestSearchRejectsBadLimit(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	if code, _ := get(t, h, "/search?q=x&n=hundred"); code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", code)
	}
}

func TestGetLeaf(t *testing.T) {
	fs := &fakeSearcher{callback: func(q string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
		return []byte(`{}`), nil
	}}
	h := NewServer(fs, 1)
	if code, _ := get(t, h, "/get"); code != http.StatusBadRequest {
		t.Fatalf("missing id code = %d, want 400", code)
	}
	code, body := get(t, h, "/get?id=leaf-1&body=1")
	if code != http.StatusOK {
		t.Fatalf("get code = %d, want 200 body=%s", code, body)
	}
	if !strings.Contains(string(body), "leaf-1") {
		t.Fatalf("get body %s missing id", body)
	}
}

func TestStatsAuditIngest(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	for _, path := range []string{"/stats", "/audit", "/ingest"} {
		code, body := get(t, h, path)
		if code != http.StatusOK {
			t.Fatalf("%s code = %d, want 200 (%s)", path, code, body)
		}
		if !json.Valid(body) {
			t.Fatalf("%s body not json: %s", path, body)
		}
	}
}

func TestIngestIsAddNotRebuildHint(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	code, body := get(t, h, "/ingest")
	if code != http.StatusOK {
		t.Fatalf("GET /ingest code = %d body=%s", code, body)
	}
	if strings.Contains(string(body), `"add":"v2"`) || strings.Contains(string(body), "write is v2") {
		t.Fatalf("GET /ingest still a v2 hint: %s", body)
	}
	if !strings.Contains(string(body), "bin/brain/add.go") {
		t.Fatalf("GET /ingest should name add.go: %s", body)
	}
	code, body = postJSON(t, h, "/ingest", `{"text":"hello","root":"info","source":"t"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /ingest code = %d body=%s", code, body)
	}
	if !strings.Contains(string(body), "fake-leaf") {
		t.Fatalf("POST /ingest should add: %s", body)
	}
}

func TestHTTPPackageDoesNotExecPython(t *testing.T) {
	raw, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))
	if strings.Contains(lower, "python3") || strings.Contains(lower, "bin/kb/search") {
		t.Fatal("httpapi must not exec Python or bin/kb/search")
	}
}

func TestDefaultSearchCmdIsBrainNotPython(t *testing.T) {
	cmd := defaultSearchCmd("/repo", &config.Config{})
	if strings.Contains(strings.ToLower(cmd), "python") {
		t.Fatalf("search path still python: %s", cmd)
	}
	if !strings.Contains(cmd, "brain") {
		t.Fatalf("search path must be the Go brain binary, got %s", cmd)
	}
}

func TestLeafsEndpointPassesFilters(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	code, body := get(t, h, "/leafs?root=facts&type=fact&source=mcp-agent&q=matrix&n=5")
	if code != http.StatusOK {
		t.Fatalf("code = %d body=%s", code, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("body not json: %v (%s)", err, body)
	}
	if out["root"] != "facts" || out["type"] != "fact" || out["source"] != "mcp-agent" {
		t.Fatalf("filters not passed: %v", out)
	}
	if out["text"] != "matrix" || out["limit"] != float64(5) {
		t.Fatalf("text/limit not passed: %v", out)
	}
	if code, _ := get(t, h, "/leafs?n=0"); code != http.StatusBadRequest {
		t.Fatalf("bad n code = %d, want 400", code)
	}
}

func TestEdgesEndpointAdjacency(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	if code, _ := get(t, h, "/edges"); code != http.StatusBadRequest {
		t.Fatalf("missing id code = %d, want 400", code)
	}
	code, body := get(t, h, "/edges?id=leaf-a")
	if code != http.StatusOK {
		t.Fatalf("code = %d body=%s", code, body)
	}
	if !strings.Contains(string(body), "leaf-a") {
		t.Fatalf("edges body missing id: %s", body)
	}
}

func TestAddEdgeEndpoint(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	code, body := postJSON(t, h, "/addedge", `{"from":"a","to":"b","type":"synapse"}`)
	if code != http.StatusOK {
		t.Fatalf("code = %d body=%s", code, body)
	}
	if !strings.Contains(string(body), `"from":"a"`) || !strings.Contains(string(body), `"to":"b"`) {
		t.Fatalf("addedge body %s", body)
	}
}

func TestPathEndpoint(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	if code, _ := get(t, h, "/path?from=a"); code != http.StatusBadRequest {
		t.Fatalf("missing to code = %d, want 400", code)
	}
	code, body := get(t, h, "/path?from=a&to=c&max=5")
	if code != http.StatusOK {
		t.Fatalf("code = %d body=%s", code, body)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("body not json: %v (%s)", err, body)
	}
	if out["from"] != "a" || out["to"] != "c" || out["max"] != float64(5) {
		t.Fatalf("path args not passed: %v", out)
	}
}

func TestSynapseAuthRequiresToken(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	h.SetToken("sekrit")
	code, body := get(t, h, "/leafs?q=x")
	if code != http.StatusUnauthorized {
		t.Fatalf("no-token code = %d, want 401 body=%s", code, body)
	}
	// health stays open so orchestrators can probe liveness without auth.
	code, _ = get(t, h, "/health")
	if code != http.StatusOK {
		t.Fatalf("health code = %d, want 200", code)
	}
}

func TestSynapseAuthAcceptsBearer(t *testing.T) {
	h := NewServer(&fakeSearcher{}, 1)
	h.SetToken("sekrit")
	req := httptest.NewRequest(http.MethodGet, "/leafs?q=x", nil)
	req.Header.Set("Authorization", "Bearer sekrit")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bearer code = %d body=%s", rec.Code, rec.Body.String())
	}
	// wrong token rejected
	req = httptest.NewRequest(http.MethodGet, "/leafs?q=x", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token code = %d, want 401", rec.Code)
	}
}

func TestSearchTimeout(t *testing.T) {
	fs := &fakeSearcher{delay: time.Second}
	h := NewServer(fs, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/search?q=slow", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
		// ServeHTTP returned; body should be an error json (we don't require a
		// specific code for the pathological ctx-cancel timing, only that it
		// does not hang forever).
		return
	case <-time.After(500 * time.Millisecond):
		t.Fatal("request hung after context cancellation")
	}
}
