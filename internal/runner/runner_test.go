// Package runner tests the bounded sync-ETL pipeline (#98): Source →
// Decode(Registry) → Transform → Load with bounded channels, stats YAML and
// graceful shutdown. All tests run offline against local fixtures.
package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/etl"
	"github.com/eSlider/2dph/internal/source"
	"gopkg.in/yaml.v3"
)

// seqSource is a deterministic Source yielding its fixed id list once (cursor
// advances to a terminal value so the driver stops after the first batch).
type seqSource struct {
	name string
	kind string
	ids  []string
}

func (s *seqSource) Name() string { return s.name }

func (s *seqSource) Fetch(_ context.Context, cursor source.Cursor) ([]source.Blob, source.Cursor, error) {
	if cursor != "" {
		return nil, "", nil
	}
	kind := s.kind
	if kind == "" {
		kind = "mail"
	}
	blobs := make([]source.Blob, len(s.ids))
	for i, id := range s.ids {
		blobs[i] = source.Blob{ID: id, Kind: kind}
	}
	return blobs, "done", nil
}

// countingHandler records which paths it decoded (the decode stage) and is
// safe for concurrent use.
type countingHandler struct {
	name string
	mu   sync.Mutex
	got  map[string]bool
}

func (h *countingHandler) Name() string { return h.name }

func (h *countingHandler) Handle(_ context.Context, path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.got == nil {
		h.got = map[string]bool{}
	}
	h.got[filepath.Base(path)] = true
	return nil
}

func newCounting(name string) *countingHandler { return &countingHandler{name: name} }

func mustRegister(t *testing.T, reg *etl.Registry, h etl.Handler) {
	t.Helper()
	if err := reg.Register(h); err != nil {
		t.Fatalf("Register(%s): %v", h.Name(), err)
	}
}

// Acceptance #1: e2e smoke over a local fixture corpus. A disk source yields
// .eml files, the registry handler decodes each (decode stage), the transform
// produces an output, and the load stage receives it — proving the stages
// connect — while a YAML stats report is written.
func TestRunnerEndToEndDiskSource(t *testing.T) {
	corpus := t.TempDir()
	// Distinct content per file: the disk source addresses blobs by content
	// hash, so identical bytes would deduplicate to a single item.
	for _, name := range []string{"a.eml", "b.eml"} {
		if err := os.WriteFile(filepath.Join(corpus, name), []byte("Subject: hi\n\nbody "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h := newCounting("mail")
	reg := etl.NewRegistry()
	mustRegister(t, reg, h)

	var (
		loadMu sync.Mutex
		loads  []string
	)
	statsPath := filepath.Join(t.TempDir(), "stats.yml")
	cfg := Config{
		Source:    &source.Disk{Root: corpus},
		Registry:  reg,
		StatePath: filepath.Join(t.TempDir(), "disk.json"),
		StatsPath: statsPath,
		Workers:   2,
		Buffer:    2,
		Transform: func(_ context.Context, it Item) (string, error) { return it.Blob.Path, nil },
		Load: func(_ context.Context, it Item) error {
			loadMu.Lock()
			loads = append(loads, filepath.Base(it.Out))
			loadMu.Unlock()
			return nil
		},
	}

	rep, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Source.New != 2 {
		t.Fatalf("Source.New = %d, want 2", rep.Source.New)
	}
	if len(rep.Handlers) != 1 || rep.Handlers[0].Handler != "mail" || rep.Handlers[0].OK != 2 {
		t.Fatalf("handler stats = %+v, want single mail ok=2", rep.Handlers)
	}
	// Every fixture decoded by the handler and every output reached the load.
	h.mu.Lock()
	for _, name := range []string{"a.eml", "b.eml"} {
		if !h.got[name] {
			t.Fatalf("handler never decoded %s (got %v)", name, h.got)
		}
	}
	h.mu.Unlock()
	loadMu.Lock()
	if len(loads) != 2 {
		t.Fatalf("load received %d items, want 2: %v", len(loads), loads)
	}
	loadMu.Unlock()

	// Stats YAML file exists and round-trips the report fields.
	b, err := os.ReadFile(statsPath)
	if err != nil {
		t.Fatalf("read stats YAML: %v", err)
	}
	var got Report
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatalf("stats YAML not valid: %v\n%s", err, b)
	}
	if got.Source.New != 2 || len(got.Handlers) != 1 || got.Handlers[0].OK != 2 {
		t.Fatalf("round-tripped report = %+v, want New=2 mail ok=2", got)
	}
}

// Acceptance #2: bounded backpressure. With a small buffer and slow consumers
// the number of items in flight never exceeds Buffer+Workers — the pipeline
// does not buffer unboundedly — and every item still completes.
func TestRunnerBackpressureBoundedInFlight(t *testing.T) {
	const n = 64
	h := newCounting("mail")
	reg := etl.NewRegistry()
	mustRegister(t, reg, h)

	var inflight, maxInFlight atomic.Int64
	load := func(_ context.Context, it Item) error {
		v := inflight.Add(1)
		for {
			cur := maxInFlight.Load()
			if v <= cur || maxInFlight.CompareAndSwap(cur, v) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond) // slow consumer
		inflight.Add(-1)
		return nil
	}

	const workers, buffer = 2, 2
	cfg := Config{
		Source:    &seqSource{name: "seq", ids: seqIDs(n)},
		Registry:  reg,
		StatePath: filepath.Join(t.TempDir(), "seq.json"),
		Workers:   workers,
		Buffer:    buffer,
		Load:      load,
	}
	rep, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Source.New != n {
		t.Fatalf("Source.New = %d, want %d", rep.Source.New, n)
	}
	if got := int(maxInFlight.Load()); got > workers+buffer {
		t.Fatalf("max in-flight = %d, exceeds bounded Buffer+Workers = %d", got, workers+buffer)
	}
	if rep.Handlers[0].OK != n {
		t.Fatalf("handler ok = %d, want %d", rep.Handlers[0].OK, n)
	}
}

// Acceptance #3: graceful shutdown. Cancelling the context mid-run drains and
// returns cleanly with context.Canceled (no deadlock, no leak under -race).
func TestRunnerGracefulShutdownOnCancel(t *testing.T) {
	h := newCounting("mail")
	reg := etl.NewRegistry()
	mustRegister(t, reg, h)

	var processed atomic.Int64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	load := func(c context.Context, _ Item) error {
		if processed.Add(1) == 3 {
			cancel() // stop the run mid-flight
		}
		time.Sleep(time.Millisecond)
		return nil
	}
	cfg := Config{
		Source:    &seqSource{name: "seq", ids: seqIDs(100)},
		Registry:  reg,
		StatePath: filepath.Join(t.TempDir(), "seq.json"),
		Workers:   4,
		Buffer:    4,
		Load:      load,
	}

	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, cfg)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not shut down after context cancel (leak/deadlock)")
	}
}

// Acceptance #4: unknown handler kind is an error — no silent skip.
func TestRunnerUnknownKindIsError(t *testing.T) {
	reg := etl.NewRegistry()
	mustRegister(t, reg, newCounting("mail"))
	cfg := Config{
		Source:    &seqSource{name: "seq", kind: "no-such-handler", ids: []string{"x"}},
		Registry:  reg,
		StatePath: filepath.Join(t.TempDir(), "seq.json"),
	}
	if _, err := Run(context.Background(), cfg); err == nil {
		t.Fatal("expected error for blob kind without a registered handler")
	}
}

func seqIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "id-" + itoa(i)
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}
