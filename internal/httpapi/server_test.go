package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSearcher is an injectable Searcher for tests (no python involved).
type fakeSearcher struct {
	mu       sync.Mutex
	delay    time.Duration
	calls    int
	active   atomic.Int32
	maxSeen  atomic.Int32
	callback func(q string, limit int) ([]byte, error)
}

func (f *fakeSearcher) Search(ctx context.Context, query string, limit int) ([]byte, error) {
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
		return f.callback(query, limit)
	}
	return []byte(`{"query":"` + query + `","count":0,"results":[]}`), nil
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
	fs := &fakeSearcher{callback: func(q string, limit int) ([]byte, error) {
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
