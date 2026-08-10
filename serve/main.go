// Package main serves the 2dph brain over HTTP.
//
// Async by design: every request runs on its own goroutine, and CPU-heavy
// searches are serialized through a bounded worker pool (a counting
// semaphore) so N requests can't spawn N Python interpreters at once.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Searcher interface {
	Search(ctx context.Context, query string, limit int) ([]byte, error)
}

type Server struct {
	searcher  Searcher
	semaphore chan struct{}
}

const defaultPort = 8630

func NewServer(searcher Searcher, workers int) http.Handler {
	return &Server{
		searcher:  searcher,
		semaphore: make(chan struct{}, workers),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/health":
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case r.URL.Path == "/search":
		s.handleSearch(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "q required"})
		return
	}
	limit := 10
	if raw := r.URL.Query().Get("n"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "n must be int 1..100"})
			return
		}
		limit = n
	}

	// Worker pool: block until a slot frees, so burst concurrency still
	// bounds memory (no unbounded python processes).
	select {
	case s.semaphore <- struct{}{}:
		defer func() { <-s.semaphore }()
	case <-r.Context().Done():
		return
	}

	body, err := s.searcher.Search(r.Context(), q, limit)
	if err != nil {
		writeJSON(w, http.StatusGatewayTimeout, map[string]any{"error": err.Error()})
		return
	}
	writeRaw(w, http.StatusOK, body)
}

func writeJSON(w http.ResponseWriter, code int, obj any) {
	body, _ := json.Marshal(obj)
	writeRaw(w, code, body)
}

func writeRaw(w http.ResponseWriter, code int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(code)
	w.Write(body)
}

// brainSearcher shells out to bin/kb/search --json. A single python search
// is bounded and short-lived; the worker pool keeps at most N live.
type brainSearcher struct {
	cmdPath string
	timeout time.Duration
}

func (b *brainSearcher) Search(ctx context.Context, query string, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.cmdPath, "--json", "-n", strconv.Itoa(limit), query)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errors.New("search backend failed: " + strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func main() {
	searchPath := os.Getenv("KB_SEARCH_CMD")
	if searchPath == "" {
		searchPath = filepath.Join("bin", "kb", "search")
	}
	workers := 4
	if raw := os.Getenv("KB_WORKERS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			workers = n
		}
	}
	port := defaultPort
	if raw := os.Getenv("KB_PORT"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			port = n
		}
	}

	searcher := &brainSearcher{cmdPath: searchPath, timeout: 60 * time.Second}
	handler := NewServer(searcher, workers)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	log.Printf("serve: %s (workers=%d)", addr, workers)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}