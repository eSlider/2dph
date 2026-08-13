package websearch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigRequiresURL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "search.env")
	if err := os.WriteFile(p, []byte("BRAIN_SEARCH_USER=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(p); err == nil {
		t.Fatal("expected missing URL error")
	}
}

func TestLoadConfigOptionalAuth(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "search.env")
	if err := os.WriteFile(p, []byte("BRAIN_SEARCH_URL=http://127.0.0.1:8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "http://127.0.0.1:8080" || c.User != "" || c.Pass != "" {
		t.Fatalf("%+v", c)
	}
}

func TestFetchJSONNoBasicAuth(t *testing.T) {
	payload := Payload{Query: "x", Results: []RawHit{{Title: "t", URL: "http://example.com", Content: "c", Engine: "bing"}}}
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Query().Get("format") != "json" || r.URL.Query().Get("q") != "x" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()
	got, err := Fetch(srv.Client(), Config{URL: srv.URL}, "x", nil, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth != "" {
		t.Fatalf("Authorization = %q, want empty for local instance", sawAuth)
	}
	if Classify(got) != StatusOK {
		t.Fatalf("classify = %s", Classify(got))
	}
}

func TestFetchSendsBasicAuthWhenConfigured(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"query":"x","results":[]}`))
	}))
	defer srv.Close()
	_, err := Fetch(srv.Client(), Config{URL: srv.URL, User: "u", Pass: "p"}, "x", nil, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if sawAuth == "" {
		t.Fatal("expected Basic auth")
	}
}
