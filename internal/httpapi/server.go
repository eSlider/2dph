// Package httpapi serves the 2dph brain over HTTP.
//
// Async by design: every request runs on its own goroutine, and CPU-heavy
// searches are serialized through a bounded worker pool so N requests can't
// spawn N backends at once.
//
// Used by bin/brain/serve.go. Tests inject a fake API (no exec, no ladybug).
package httpapi

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

// API is the in-process brain surface. Production serve.go wires internal/brain.
type API interface {
	Search(ctx context.Context, query string, limit int) ([]byte, error)
	Get(ctx context.Context, id string, body bool) ([]byte, error)
	Stats(ctx context.Context) ([]byte, error)
	Audit(ctx context.Context) ([]byte, error)
	Ingest(ctx context.Context) ([]byte, error)
}

type Server struct {
	api       API
	semaphore chan struct{}
}

const defaultPort = 8630

var errUnimplemented = errors.New("not implemented")

func NewServer(api API, workers int) http.Handler {
	return &Server{
		api:       api,
		semaphore: make(chan struct{}, workers),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health":
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case "/search":
		s.handleSearch(w, r)
	case "/get":
		s.handleGet(w, r)
	case "/stats":
		s.handleJSON(w, r, s.api.Stats)
	case "/audit":
		s.handleJSON(w, r, s.api.Audit)
	case "/ingest":
		s.handleJSON(w, r, s.api.Ingest)
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
	if !s.acquire(w, r) {
		return
	}
	defer s.release()
	body, err := s.api.Search(r.Context(), q, limit)
	writeAPI(w, body, err)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id required"})
		return
	}
	body := r.URL.Query().Get("body") == "1" || r.URL.Query().Get("body") == "true"
	if !s.acquire(w, r) {
		return
	}
	defer s.release()
	out, err := s.api.Get(r.Context(), id, body)
	writeAPI(w, out, err)
}

func (s *Server) handleJSON(w http.ResponseWriter, r *http.Request, fn func(context.Context) ([]byte, error)) {
	if !s.acquire(w, r) {
		return
	}
	defer s.release()
	body, err := fn(r.Context())
	writeAPI(w, body, err)
}

func (s *Server) acquire(w http.ResponseWriter, r *http.Request) bool {
	select {
	case s.semaphore <- struct{}{}:
		return true
	case <-r.Context().Done():
		return false
	}
}

func (s *Server) release() { <-s.semaphore }

func writeAPI(w http.ResponseWriter, body []byte, err error) {
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, errUnimplemented) {
			code = http.StatusNotImplemented
		}
		writeJSON(w, code, map[string]any{"error": err.Error()})
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

// ExecSearcher shells out to var/bin/brain-search. Fallback when the serve
// binary is built without ladybug cgo (CI / tags=brain_serve only).
type ExecSearcher struct {
	CmdPath string
	Timeout time.Duration
}

func (b ExecSearcher) Search(ctx context.Context, query string, limit int) ([]byte, error) {
	if b.Timeout == 0 {
		b.Timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.CmdPath, "--json", "-n", strconv.Itoa(limit), query)
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

func (ExecSearcher) Get(context.Context, string, bool) ([]byte, error) {
	return nil, errUnimplemented
}
func (ExecSearcher) Stats(context.Context) ([]byte, error) { return nil, errUnimplemented }
func (ExecSearcher) Audit(context.Context) ([]byte, error) { return nil, errUnimplemented }
func (ExecSearcher) Ingest(context.Context) ([]byte, error) {
	return json.Marshal(map[string]any{
		"mode":    "rebuild",
		"command": "bin/brain/index.go --rebuild",
	})
}

func defaultSearchCmd(root string) string {
	if env := os.Getenv("KB_SEARCH_CMD"); env != "" {
		return env
	}
	return filepath.Join(root, "var", "bin", "brain-search")
}

func workersAndPort() (int, int) {
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
	return workers, port
}

// Run starts the HTTP server with an injected API (in-process brain, or ExecSearcher).
func Run(api API) {
	if api == nil {
		root := os.Getenv("KB_ROOT")
		api = ExecSearcher{CmdPath: defaultSearchCmd(root), Timeout: 60 * time.Second}
	}
	workers, port := workersAndPort()
	handler := NewServer(api, workers)
	addr := "127.0.0.1:" + strconv.Itoa(port)
	log.Printf("serve: %s (workers=%d)", addr, workers)
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}
