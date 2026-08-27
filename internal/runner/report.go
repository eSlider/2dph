package runner

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eSlider/2dph/internal/source"
)

// Report is the per-run aggregate, serialized as the stats YAML (#98):
//
//	startedAt: 2026-08-25T...
//	duration: 1.2s
//	source:
//	  fetched: 100
//	  new: 42
//	  skipped: 58
//	handlers:
//	  - handler: mail
//	    ok: 42
//	    failed: 0
type Report struct {
	StartedAt time.Time      `yaml:"startedAt"`
	Duration  time.Duration  `yaml:"duration"`
	Source    source.Stats   `yaml:"source"`
	Handlers  []HandlerStats `yaml:"handlers"`
	Error     string         `yaml:"error,omitempty"`
}

// HandlerStats counts how many blobs one handler decoded (ok) and how many
// failed (failed). Failed items are also the first pipeline error, so a
// non-zero Failed aborts the run.
type HandlerStats struct {
	Handler string `yaml:"handler"`
	OK      int64  `yaml:"ok"`
	Failed  int64  `yaml:"failed"`
}

// handlerCount holds atomic per-handler counters.
type handlerCount struct {
	ok, failed atomic.Int64
}

// handlerStats is a concurrency-safe key → counter map.
type handlerStats struct {
	mu sync.Mutex
	m  map[string]*handlerCount
}

func newHandlerStats() *handlerStats { return &handlerStats{m: make(map[string]*handlerCount)} }

func (s *handlerStats) counter(kind string) *handlerCount {
	if kind == "" {
		kind = "unknown"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[kind]
	if !ok {
		c = &handlerCount{}
		s.m[kind] = c
	}
	return c
}

func (s *handlerStats) ok(kind string)     { s.counter(kind).ok.Add(1) }
func (s *handlerStats) failed(kind string) { s.counter(kind).failed.Add(1) }

// snapshot returns a deterministic, sorted snapshot of the per-handler stats.
func (s *handlerStats) snapshot() []HandlerStats {
	s.mu.Lock()
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]HandlerStats, 0, len(keys))
	for _, k := range keys {
		c := s.m[k]
		out = append(out, HandlerStats{Handler: k, OK: c.ok.Load(), Failed: c.failed.Load()})
	}
	s.mu.Unlock()
	return out
}

// WriteYAML persists a Report as the stats YAML file, atomically enough for
// the daemon use (temp + rename in the same directory).
func WriteYAML(path string, rep *Report) error {
	b, err := yaml.Marshal(rep)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".stats-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
