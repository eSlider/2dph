//go:build cgo && system_ladybug

// Process-wide embedding-model singleton shared by every in-process consumer:
// /ingest, the /search in-process fallback and the ANN extract path.
//
// Every path used to LoadModel() per request. StaticModel.Close freed only the
// tokenizer, not the safetensors matrix, so each request retained ~490 MiB
// inuse (~980 MiB allocated: read buffer + matrix) until GC pressure caught up.
// Measured fallout: 3 /ingest requests grew RSS 3.5→6.1 GiB (#109) and 13
// /search requests grew RSS 147 MiB → 6.8 GiB with 13.8 GiB allocated in
// loadMatrix (#335). The cache loads once per process and hands the same
// instance to every request; it exposes no Close, so the shared model can never
// be closed mid-flight or after a request. A failed load is not cached — the
// next request retries.
package brain

import "sync"

// embedder is the model slice consumers need. Deliberately no Close: callers
// cannot close the shared model through it.
type embedder interface {
	Embed(text string) ([]float64, error)
}

type embedModelCache struct {
	mu     sync.Mutex
	loader func() (embedder, error)
	model  embedder
}

func newEmbedModelCache(loader func() (embedder, error)) *embedModelCache {
	return &embedModelCache{loader: loader}
}

func (c *embedModelCache) get() (embedder, error) {
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

var embedModel = newEmbedModelCache(func() (embedder, error) { return LoadModel() })

// getEmbedModel returns the shared process-living embedding model. Callers must
// never close it — the process owns it for its whole lifetime.
func getEmbedModel() (embedder, error) {
	return embedModel.get()
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
