//go:build cgo && system_ladybug

package brain

import (
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/eSlider/2dph/internal/config"
)

// searchLoadIterations mirrors the #335 acceptance load (N≥200 searches).
const searchLoadIterations = 200

// countingEmbedder is a stub embedder that records Close calls, so tests can
// prove the shared model is never closed mid-flight.
type countingEmbedder struct {
	mu         sync.Mutex
	closeCalls int
}

func (c *countingEmbedder) Embed(_ string) ([]float64, error) {
	return make([]float64, 8), nil
}

func (c *countingEmbedder) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeCalls++
	return nil
}

func (c *countingEmbedder) closes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeCalls
}

// stubEmbedModel replaces the process-wide model cache with loader and restores
// the previous cache when the test ends. Lets the search/ingest paths run
// offline and count model loads.
func stubEmbedModel(t *testing.T, loader func() (embedder, error)) {
	t.Helper()
	prev := embedModel
	embedModel = newEmbedModelCache(loader)
	t.Cleanup(func() { embedModel = prev })
}

func TestEmbedModelSingletonLoadsOnce(t *testing.T) {
	loads := 0
	cache := newEmbedModelCache(func() (embedder, error) {
		loads++
		return &countingEmbedder{}, nil
	})

	first, err := cache.get()
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	second, err := cache.get()
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if first != second {
		t.Fatal("singleton cache returned different instances")
	}
	if loads != 1 {
		t.Fatalf("loader called %d times, want 1", loads)
	}
}

// Two consecutive /ingest requests must share one loaded model: the request
// path is cache.get() + embedLeafs, exactly what HTTP.Ingest runs minus the
// Ladybug write section (#109 territory).
func TestIngestRequestsShareSingleLoad(t *testing.T) {
	loads := 0
	cache := newEmbedModelCache(func() (embedder, error) {
		loads++
		return &countingEmbedder{}, nil
	})
	req := func(texts ...string) error {
		model, err := cache.get()
		if err != nil {
			return err
		}
		leafs := make([]LeafInput, len(texts))
		for i, txt := range texts {
			leafs[i] = LeafInput{Text: txt}
		}
		return embedLeafs(model, leafs)
	}

	if err := req("first request leaf"); err != nil {
		t.Fatalf("request 1: %v", err)
	}
	if err := req("second request leaf a", "second request leaf b"); err != nil {
		t.Fatalf("request 2: %v", err)
	}
	if loads != 1 {
		t.Fatalf("model loaded %d times across 2 requests, want 1", loads)
	}
}

func TestEmbedModelSingletonConcurrentLoadsOnce(t *testing.T) {
	loads := 0
	var mu sync.Mutex
	cache := newEmbedModelCache(func() (embedder, error) {
		mu.Lock()
		loads++
		mu.Unlock()
		return &countingEmbedder{}, nil
	})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := cache.get(); err != nil {
				t.Errorf("concurrent get: %v", err)
			}
		}()
	}
	wg.Wait()

	if loads != 1 {
		t.Fatalf("concurrent loader called %d times, want 1", loads)
	}
}

func TestEmbedModelNoCloseOfSharedModel(t *testing.T) {
	em := &countingEmbedder{}
	cache := newEmbedModelCache(func() (embedder, error) { return em, nil })

	for i := 0; i < 3; i++ {
		if _, err := cache.get(); err != nil {
			t.Fatalf("get %d: %v", i, err)
		}
	}
	// The shared model lives for the process; nothing may close it between
	// or after requests (the old defer model.Close() freed the tokenizer out
	// from under the next request and leaked the matrix).
	if em.closes() != 0 {
		t.Fatalf("shared model closed %d times, want 0", em.closes())
	}
}

// A failed load must not poison the singleton: the next request retries the
// loader instead of failing forever.
func TestEmbedModelCacheRetriesAfterLoaderError(t *testing.T) {
	calls := 0
	cache := newEmbedModelCache(func() (embedder, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("weights not downloaded yet")
		}
		return &countingEmbedder{}, nil
	})

	if _, err := cache.get(); err == nil {
		t.Fatal("first get: want loader error")
	}
	m, err := cache.get()
	if err != nil {
		t.Fatalf("second get after failed load: %v", err)
	}
	if m == nil {
		t.Fatal("second get returned nil model")
	}
	if calls != 2 {
		t.Fatalf("loader called %d times, want 2 (retry after failure)", calls)
	}
}

// TestSearchEmbedLoadsModelOnce is the #335 regression: N sequential search
// embeds through the production helper must load the model exactly once and
// never close it. Before the fix every /search ran loadModel()+Close, so this
// test fails with N loads (and ~N × 980 MiB allocated in loadMatrix).
func TestSearchEmbedLoadsModelOnce(t *testing.T) {
	loads := 0
	em := &countingEmbedder{}
	stubEmbedModel(t, func() (embedder, error) {
		loads++
		return em, nil
	})

	for i := 0; i < searchLoadIterations; i++ {
		if _, err := embedInProcess("docker compose"); err != nil {
			t.Fatalf("embed %d: %v", i, err)
		}
	}
	if loads != 1 {
		t.Fatalf("model loaded %d times over %d searches, want 1", loads, searchLoadIterations)
	}
	if em.closes() != 0 {
		t.Fatalf("shared search model closed %d times over %d searches, want 0", em.closes(), searchLoadIterations)
	}
}

// TestEmbedQueryFallbackUsesSharedModel exercises the real embedQuery fallback
// (daemon disabled, nothing listening) and proves it reaches the shared cache
// once instead of loading the model per request (#335).
func TestEmbedQueryFallbackUsesSharedModel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // free port: tryDaemon gets a fast connection refused

	prevCfg := brainCfg()
	cfg := config.Defaults()
	cfg.Root = t.TempDir()
	cfg.SearchNoDaemon = true
	cfg.SearchDaemonPort = port
	Configure(&cfg)
	t.Cleanup(func() { Configure(&prevCfg) })

	loads := 0
	em := &countingEmbedder{}
	stubEmbedModel(t, func() (embedder, error) {
		loads++
		return em, nil
	})

	for i := 0; i < 5; i++ {
		if _, err := embedQuery("docker compose"); err != nil {
			t.Fatalf("embedQuery %d: %v", i, err)
		}
	}
	if loads != 1 {
		t.Fatalf("embedQuery loaded the model %d times over 5 queries, want 1", loads)
	}
	if em.closes() != 0 {
		t.Fatalf("embedQuery closed the shared model %d times, want 0", em.closes())
	}
}

// TestStaticModelCloseFreesMatrix — Close must release the safetensors matrix
// (~490 MiB), not just the tokenizer (#335).
func TestStaticModelCloseFreesMatrix(t *testing.T) {
	m := &StaticModel{
		mat:       make([]float32, 16),
		vocabSize: 4,
		dim:       4,
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if m.mat != nil {
		t.Fatal("Close did not release the embedding matrix")
	}
}

func TestEmbedLeafsSkipsPreembedded(t *testing.T) {
	em := &countingEmbedder{}
	pre := make([]float64, EmbedDim)
	pre[0] = 0.5
	leafs := []LeafInput{
		{Text: "already embedded", Embedding: pre},
		{Text: "needs embedding"},
	}
	if err := embedLeafs(em, leafs); err != nil {
		t.Fatalf("embedLeafs: %v", err)
	}
	if len(leafs[0].Embedding) != EmbedDim || leafs[0].Embedding[0] != 0.5 {
		t.Fatal("pre-embedded leaf was overwritten")
	}
	if len(leafs[1].Embedding) != 8 {
		t.Fatalf("leaf not embedded: len=%d want 8", len(leafs[1].Embedding))
	}
}
