//go:build cgo && system_ladybug

// Process-wide singleton for the /ingest embedding model.
//
// Ingest used to LoadModel() per request; StaticModel.Close frees only the
// tokenizer, not the safetensors matrix, so every request retained ~0.9 GiB
// until GC pressure caught up (3 ingests: RSS 3.5→6.1 GiB). The cache loads
// once per process and hands the same instance to every request; it exposes
// no Close, so the shared model can never be closed mid-flight or after a
// request. A failed load is not cached — the next request retries.
package brain

import "sync"

// embedder is the ingest-facing slice of StaticModel. Deliberately no Close:
// callers of getIngestModel cannot close the shared model through it.
type embedder interface {
	Embed(text string) ([]float64, error)
}

type ingestModelCache struct {
	mu     sync.Mutex
	loader func() (embedder, error)
	model  embedder
}

func newIngestModelCache(loader func() (embedder, error)) *ingestModelCache {
	return &ingestModelCache{loader: loader}
}

func (c *ingestModelCache) get() (embedder, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.model != nil {
		return c.model, nil
	}
	m, err := c.loader()
	if err != nil {
		return nil, err // not cached: next caller retries
	}
	c.model = m
	return m, nil
}

var ingestModel = newIngestModelCache(func() (embedder, error) { return LoadModel() })

// getIngestModel returns the shared process-living embedding model for /ingest.
func getIngestModel() (embedder, error) {
	return ingestModel.get()
}

// embedLeafs fills missing embeddings in place; leafs that already carry one
// (e.g. from facts extraction) are left untouched.
func embedLeafs(emb embedder, leafs []LeafInput) error {
	for i := range leafs {
		if len(leafs[i].Embedding) > 0 {
			continue
		}
		vec, err := emb.Embed(leafs[i].Text)
		if err != nil {
			return err
		}
		leafs[i].Embedding = vec
	}
	return nil
}
