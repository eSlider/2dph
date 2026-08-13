package websearch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLookupRefusesPIIWithoutFetch(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
	}))
	defer srv.Close()
	out := Lookup(context.Background(), "Personalnummer 12", LookupOpt{
		EnvPath: writeEnv(t, srv.URL),
		CachePath: filepath.Join(t.TempDir(), "c.sqlite"),
		Client:    srv.Client(),
		Sleep:     func(context.Context, time.Duration) error { return nil },
	})
	if out.Status != StatusRefused {
		t.Fatalf("status = %s", out.Status)
	}
	if hits != 0 {
		t.Fatal("PII query left the host")
	}
}

func TestLookupSkipsWhenNoConfig(t *testing.T) {
	out := Lookup(context.Background(), "LadybugDB", LookupOpt{
		EnvPath:   filepath.Join(t.TempDir(), "missing.env"),
		CachePath: filepath.Join(t.TempDir(), "c.sqlite"),
		Sleep:     func(context.Context, time.Duration) error { return nil },
	})
	if out.Status != StatusSkipped {
		t.Fatalf("status = %s", out.Status)
	}
}

func TestLookupFetchesOnceAndCaches(t *testing.T) {
	hits := 0
	payload := Payload{Query: "x", Results: []RawHit{{Title: "t", URL: "http://example.com", Content: "c", Engine: "bing"}}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		json.NewEncoder(w).Encode(payload)
	}))
	defer srv.Close()
	opt := LookupOpt{
		EnvPath:   writeEnv(t, srv.URL),
		CachePath: filepath.Join(t.TempDir(), "c.sqlite"),
		Client:    srv.Client(),
		Now:       func() float64 { return 1_000 },
		Sleep:     func(context.Context, time.Duration) error { return nil },
	}
	a := Lookup(context.Background(), "LadybugDB", opt)
	b := Lookup(context.Background(), "LadybugDB", opt)
	if a.Status != StatusOK || b.Status != StatusOK {
		t.Fatalf("a=%s b=%s", a.Status, b.Status)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (second from cache)", hits)
	}
	if !b.Cached {
		t.Fatal("second lookup not cached")
	}
}

func TestLookupEmptyIsThrottled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"query":"x","results":[]}`))
	}))
	defer srv.Close()
	out := Lookup(context.Background(), "LadybugDB", LookupOpt{
		EnvPath:   writeEnv(t, srv.URL),
		CachePath: filepath.Join(t.TempDir(), "c.sqlite"),
		Client:    srv.Client(),
		Now:       func() float64 { return 1_000 },
		Sleep:     func(context.Context, time.Duration) error { return nil },
	})
	if out.Status != StatusThrottled {
		t.Fatalf("status = %s", out.Status)
	}
}

func writeEnv(t *testing.T, url string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "search.env")
	if err := os.WriteFile(p, []byte("BRAIN_SEARCH_URL="+url+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
