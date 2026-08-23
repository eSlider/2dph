//go:build cgo && system_ladybug

package brain

import (
	"errors"
	"sync"
	"testing"
)

// countingEmbedder is a stub embedder that records Close calls, so tests can
// prove the shared ingest model is never closed mid-flight.
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

func TestIngestModelSingletonLoadsOnce(t *testing.T) {
	loads := 0
	cache := newIngestModelCache(func() (embedder, error) {
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
	cache := newIngestModelCache(func() (embedder, error) {
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

func TestIngestModelSingletonConcurrentLoadsOnce(t *testing.T) {
	loads := 0
	var mu sync.Mutex
	cache := newIngestModelCache(func() (embedder, error) {
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

func TestIngestModelNoCloseOfSharedModel(t *testing.T) {
	em := &countingEmbedder{}
	cache := newIngestModelCache(func() (embedder, error) { return em, nil })

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
func TestIngestModelCacheRetriesAfterLoaderError(t *testing.T) {
	calls := 0
	cache := newIngestModelCache(func() (embedder, error) {
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
