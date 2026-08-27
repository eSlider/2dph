package bench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// mcpServer is an httptest fixture serving the JSON-RPC search tool.
func mcpServer(t *testing.T, hitsJSON string, isError bool, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if calls != nil {
			calls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		content := `{"results":[{"id":"1","text":"BM25 ranks best-first","root":"info"}]}`
		if hitsJSON != "" {
			content = hitsJSON
		}
		resp := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"text":` +
			jsonString(content) + `,"isError":` + boolStr(isError) + `}]}}`
		if isError {
			resp = `{"jsonrpc":"2.0","id":1,"result":{"content":[{"text":"search failed","isError":true}]}}`
		}
		w.Write([]byte(resp))
	}))
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestHTTPSearcherSearch(t *testing.T) {
	var calls atomic.Int64
	srv := mcpServer(t, "", false, &calls)
	defer srv.Close()

	s := NewHTTPSearcher(HTTPConfig{BaseURL: srv.URL})
	defer s.Close()
	hits, err := s.Search(context.Background(), "hybrid fts vector", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "1" || !strings.Contains(hits[0].Text, "BM25") {
		t.Errorf("hits = %+v", hits)
	}
	if calls.Load() != 1 {
		t.Errorf("calls=%d, want 1", calls.Load())
	}
	if s.Name() != srv.URL {
		t.Errorf("Name=%q", s.Name())
	}
}

func TestHTTPSearcherRPCError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`))
	}))
	defer srv.Close()
	s := NewHTTPSearcher(HTTPConfig{BaseURL: srv.URL})
	defer s.Close()
	if _, err := s.Search(context.Background(), "q", 5); err == nil {
		t.Error("rpc error must surface")
	}
}

func TestHTTPSearcherToolError(t *testing.T) {
	srv := mcpServer(t, "", true, nil)
	defer srv.Close()
	s := NewHTTPSearcher(HTTPConfig{BaseURL: srv.URL})
	defer s.Close()
	if _, err := s.Search(context.Background(), "q", 5); err == nil {
		t.Error("tool isError must surface")
	}
}

func TestHTTPSearcherHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	s := NewHTTPSearcher(HTTPConfig{BaseURL: srv.URL})
	defer s.Close()
	if _, err := s.Search(context.Background(), "q", 5); err == nil {
		t.Error("http 500 must surface")
	}
}

func TestHTTPSearcherCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := mcpServer(t, "", false, nil)
	defer srv.Close()
	s := NewHTTPSearcher(HTTPConfig{BaseURL: srv.URL})
	defer s.Close()
	if _, err := s.Search(ctx, "q", 5); err == nil {
		t.Error("cancelled ctx must error")
	}
}
