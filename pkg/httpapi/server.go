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
	"io"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/config"
)

// API is the in-process brain surface. Production serve.go wires internal/brain.
type API interface {
	Search(ctx context.Context, query string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error)
	Get(ctx context.Context, id string, body bool) ([]byte, error)
	Stats(ctx context.Context) ([]byte, error)
	Audit(ctx context.Context) ([]byte, error)
	Ingest(ctx context.Context, body []byte) ([]byte, error)
	// Synapse Matrix surface (issue #82): leafs + edges as a service.
	Leafs(ctx context.Context, root, typ, source, text string, limit int) ([]byte, error)
	Edges(ctx context.Context, id string) ([]byte, error)
	AddEdge(ctx context.Context, body []byte) ([]byte, error)
	Path(ctx context.Context, from, to string, max int) ([]byte, error)
}

type Server struct {
	api       API
	semaphore chan struct{}
	token     string // when set, every route except /health requires Bearer auth.
}

const defaultPort = 8630

var errUnimplemented = errors.New("not implemented")

func NewServer(api API, workers int) *Server {
	return &Server{
		api:       api,
		semaphore: make(chan struct{}, workers),
	}
}

// SetToken enables Bearer-token auth on every route except /health. A zero
// token leaves the server open (safe when bound to 127.0.0.1 only).
func (s *Server) SetToken(token string) *Server {
	s.token = token
	return s
}

// Auth check: when a token is configured, requests must carry
// `Authorization: Bearer <token>`. /health stays open for orchestrators.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	if s.token == "" || r.URL.Path == PathHealth {
		return true
	}
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(auth) > len(prefix) && auth[:len(prefix)] == prefix &&
		strings.TrimSpace(auth[len(prefix):]) == s.token {
		return true
	}
	writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
	return false
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	switch r.URL.Path {
	case PathHealth:
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	case PathSearch:
		s.handleSearch(w, r)
	case PathGet:
		s.handleGet(w, r)
	case PathStats:
		s.handleJSON(w, r, s.api.Stats)
	case PathAudit:
		s.handleJSON(w, r, s.api.Audit)
	case PathIngest:
		s.handleIngest(w, r)
	case PathLeafs:
		s.handleLeafs(w, r)
	case PathEdges:
		s.handleEdges(w, r)
	case PathAddEdge:
		s.handleAddEdge(w, r)
	case PathPath:
		s.handlePath(w, r)
	case PathOpenAPI:
		s.handleOpenAPI(w, r)
	case PathMCP:
		s.handleMCP(w, r)
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
	asOf := strings.TrimSpace(r.URL.Query().Get("as_of"))
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	sort := strings.TrimSpace(r.URL.Query().Get("sort"))
	noWeb := r.URL.Query().Get("noweb") == "1" || r.URL.Query().Get("noweb") == "true"
	if !s.acquire(w, r) {
		return
	}
	defer s.release()
	body, err := s.api.Search(r.Context(), q, limit, asOf, root, sort, noWeb)
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

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	var raw []byte
	if r.Method == http.MethodPost {
		b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body"})
			return
		}
		raw = b
	}
	if !s.acquire(w, r) {
		return
	}
	defer s.release()
	body, err := s.api.Ingest(r.Context(), raw)
	writeAPI(w, body, err)
}

func (s *Server) handleLeafs(w http.ResponseWriter, r *http.Request) {
	root := strings.TrimSpace(r.URL.Query().Get("root"))
	typ := strings.TrimSpace(r.URL.Query().Get("type"))
	source := strings.TrimSpace(r.URL.Query().Get("source"))
	text := strings.TrimSpace(r.URL.Query().Get("q"))
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
	body, err := s.api.Leafs(r.Context(), root, typ, source, text, limit)
	writeAPI(w, body, err)
}

func (s *Server) handleEdges(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "id required"})
		return
	}
	if !s.acquire(w, r) {
		return
	}
	defer s.release()
	body, err := s.api.Edges(r.Context(), id)
	writeAPI(w, body, err)
}

func (s *Server) handleAddEdge(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body"})
		return
	}
	if !s.acquire(w, r) {
		return
	}
	defer s.release()
	body, err := s.api.AddEdge(r.Context(), raw)
	writeAPI(w, body, err)
}

func (s *Server) handlePath(w http.ResponseWriter, r *http.Request) {
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" || to == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "from and to required"})
		return
	}
	max := 6
	if raw := r.URL.Query().Get("max"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 10 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "max must be int 1..10"})
			return
		}
		max = n
	}
	if !s.acquire(w, r) {
		return
	}
	defer s.release()
	body, err := s.api.Path(r.Context(), from, to, max)
	writeAPI(w, body, err)
}

func (s *Server) tryAcquire(r *http.Request) bool {
	return s.acquire(nopWriter{}, r)
}

type nopWriter struct{}

func (nopWriter) Header() http.Header       { return http.Header{} }
func (nopWriter) Write([]byte) (int, error) { return 0, nil }
func (nopWriter) WriteHeader(int)           {}

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
	Root    string
	Timeout time.Duration
}

func (b ExecSearcher) Search(ctx context.Context, query string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
	if b.Timeout == 0 {
		b.Timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()
	args := []string{"--json", "-n", strconv.Itoa(limit)}
	if root != "" {
		args = append(args, "--root", root)
	}
	if asOf != "" {
		args = append(args, "--as-of", asOf)
	}
	if sort != "" {
		args = append(args, "--sort", sort)
	}
	if noWeb {
		args = append(args, "--no-web")
	}
	args = append(args, query)
	cmd := exec.CommandContext(ctx, b.CmdPath, args...)
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
func (ExecSearcher) Leafs(context.Context, string, string, string, string, int) ([]byte, error) {
	return nil, errUnimplemented
}
func (ExecSearcher) Edges(context.Context, string) ([]byte, error) { return nil, errUnimplemented }
func (ExecSearcher) AddEdge(context.Context, []byte) ([]byte, error) { return nil, errUnimplemented }
func (ExecSearcher) Path(context.Context, string, string, int) ([]byte, error) {
	return nil, errUnimplemented
}
func (b ExecSearcher) Ingest(ctx context.Context, body []byte) ([]byte, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return json.Marshal(map[string]any{
			"mode":    "add",
			"command": "bin/brain/add.go",
			"rebuild": "bin/brain/index.go --rebuild",
		})
	}
	root := b.Root
	if root == "" {
		root = "."
	}
	cmd := exec.CommandContext(ctx, filepath.Join(root, "bin/brain/add.go"), "--json")
	cmd.Stdin = strings.NewReader(string(body))
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errors.New("add failed: " + strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func defaultSearchCmd(root string, c *config.Config) string {
	if c != nil && c.SearchCmd != "" {
		return c.SearchCmd
	}
	return filepath.Join(root, "var", "bin", "brain-search")
}

// Run starts the HTTP server with an injected API (in-process brain, or
// ExecSearcher) using the typed config for host/port/workers/pprof/search-cmd.
func Run(api API, c *config.Config) {
	if c == nil {
		c = &config.Config{}
	}
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := c.Port
	if port <= 0 {
		port = defaultPort
	}
	workers := c.Workers
	if workers <= 0 {
		workers = 4
	}
	if api == nil {
		api = ExecSearcher{CmdPath: defaultSearchCmd(c.Root, c), Root: c.Root, Timeout: 60 * time.Second}
	}
	handler := NewServer(api, workers)
	addr := host + ":" + strconv.Itoa(port)
	log.Printf("serve: %s (workers=%d)", addr, workers)
	if pprofPort := c.Pprof; pprofPort != "" {
		go func() {
			paddr := host + ":" + pprofPort
			log.Printf("pprof: %s (cpu/heap/goroutine via /debug/pprof/)", paddr)
			if err := http.ListenAndServe(paddr, nil); err != nil {
				log.Printf("pprof: %v", err)
			}
		}()
	}
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

// RunSynapse serves the Synapse Matrix surface (leafs/edges/path/addedge) for
// mcp-agent. Auth policy (#82): when a token is configured every route except
// /health requires `Authorization: Bearer <token>`; without a token the server
// refuses to bind anything but a loopback address, so the graph stays
// machine-local unless explicitly guarded.
func RunSynapse(api API, c *config.Config) {
	if c == nil {
		c = &config.Config{}
	}
	host := c.Synapse.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := c.Synapse.Port
	if port <= 0 {
		port = 8632
	}
	workers := c.Workers
	if workers <= 0 {
		workers = 4
	}
	token := strings.TrimSpace(c.Synapse.Token)
	if token == "" && !isLoopbackHost(host) {
		log.Fatalf("synapse: non-loopback bind %s requires a token (config synapse.token)", host)
	}
	if api == nil {
		api = ExecSearcher{CmdPath: defaultSearchCmd(c.Root, c), Root: c.Root, Timeout: 60 * time.Second}
	}
	handler := NewServer(api, workers)
	handler.SetToken(token)
	addr := host + ":" + strconv.Itoa(port)
	log.Printf("synapse: %s (workers=%d, auth=%v)", addr, workers, token != "")
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

// isLoopbackHost reports whether host binds to a loopback interface only.
func isLoopbackHost(host string) bool {
	if host == "" {
		return true
	}
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}
