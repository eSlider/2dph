package dockerctl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestFindOneByComposeLabels drives the real client against a real HTTP
// server (API test): filters must reach the daemon encoded as JSON labels.
func TestFindOneByComposeLabels(t *testing.T) {
	var gotFilters string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/containers/json" {
			http.NotFound(w, r)
			return
		}
		gotFilters = r.URL.Query().Get("filters")
		_ = json.NewEncoder(w).Encode([]map[string]any{{"Id": "abc123"}})
	}))
	defer srv.Close()

	c, err := NewHTTP(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	id, err := c.FindOne(context.Background(), map[string]string{
		"com.docker.compose.project": "2dph",
		"com.docker.compose.service": "brain",
	})
	if err != nil {
		t.Fatal(err)
	}
	if id != "abc123" {
		t.Fatalf("id = %q, want abc123", id)
	}
	if !strings.Contains(gotFilters, "com.docker.compose.project=2dph") {
		t.Fatalf("filters missing project label: %s", gotFilters)
	}
}

func TestFindOneRequiresExactlyOne(t *testing.T) {
	handler := func(n int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			list := make([]map[string]any, n)
			for i := range list {
				list[i] = map[string]any{"Id": "x"}
			}
			_ = json.NewEncoder(w).Encode(list)
		}
	}
	for _, n := range []int{0, 2} {
		srv := httptest.NewServer(handler(n))
		c, _ := NewHTTP(srv.URL, srv.Client())
		if _, err := c.FindOne(context.Background(), map[string]string{"a": "b"}); err == nil {
			t.Fatalf("want error for %d matches", n)
		}
		srv.Close()
	}
}

func TestStopStartStatuses(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/containers/abc/stop":
			w.WriteHeader(http.StatusNoContent)
		case "/containers/abc/start":
			w.WriteHeader(http.StatusNotModified) // already running
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c, _ := NewHTTP(srv.URL, srv.Client())
	ctx := context.Background()
	if err := c.Stop(ctx, "abc", 20); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := c.Start(ctx, "abc"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(paths) != 2 {
		t.Fatalf("paths = %v", paths)
	}
}

func TestInspectAndWaitHealthy(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		status := "starting"
		if calls >= 2 {
			status = "healthy"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"State": map[string]any{"Running": true, "Health": map[string]any{"Status": status}},
		})
	}))
	defer srv.Close()
	c, _ := NewHTTP(srv.URL, srv.Client())
	running, health, err := c.Inspect(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if !running || health != "starting" {
		t.Fatalf("inspect = %v %q", running, health)
	}
	if err := c.WaitHealthy(context.Background(), "abc", 10*time.Second); err != nil {
		t.Fatalf("wait healthy: %v", err)
	}
}

func TestWaitHealthyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"State": map[string]any{"Running": true, "Health": map[string]any{"Status": "starting"}},
		})
	}))
	defer srv.Close()
	c, _ := NewHTTP(srv.URL, srv.Client())
	if err := c.WaitHealthy(context.Background(), "abc", 50*time.Millisecond); err == nil {
		t.Fatal("want timeout error")
	}
}

func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_ping" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("OK"))
	}))
	defer srv.Close()
	c, _ := NewHTTP(srv.URL, srv.Client())
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}
