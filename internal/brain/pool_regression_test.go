//go:build cgo && system_ladybug

package brain

import (
	"sync"
	"testing"
)

// TestConcurrentSearchesDontExhaustBufferPool is a regression test for the
// QueryResult leak: conn.Query/conn.Execute results that were never closed
// pinned the Ladybug buffer pool until "Buffer manager exception: Unable to
// allocate memory! The buffer pool is full" made every endpoint 502 until a
// process restart. With defer res.Close() the pool stays evictable under load.
func TestConcurrentSearchesDontExhaustBufferPool(t *testing.T) {
	dir := t.TempDir()
	d, c, err := OpenWritable(dir + "/kb.lbug")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	defer c.Close()
	if err := InitSchema(c); err != nil {
		t.Fatal(err)
	}
	emb := make([]float64, EmbedDim)
	for i := range emb {
		emb[i] = float64(i%3) / 3.0
	}
	for i := 0; i < 10; i++ {
		if _, err := AddLeafs(c, []LeafInput{{
			Text: "pool regression leaf number " + string(rune('a'+i)),
			Source: "pool-test-" + string(rune('a'+i)), Root: "info",
			Type: "reference", Embedding: emb,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := EnsureIndexes(c); err != nil {
		t.Fatal(err)
	}

	// Drive the leak path through the package-level conn like the live serve.
	db, conn = d, c
	defer func() { db, conn = nil, nil }()

	var wg sync.WaitGroup
	for i := 0; i < 48; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 12; j++ {
				_, _ = searchHits("pool regression", "", "", 3, "")
				h := HTTP{}
				_, _ = h.Stats(nil)
				_, _ = h.Audit(nil)
			}
		}()
	}
	wg.Wait()

	// The pool must still be usable: a single search + stats must succeed
	// (previously every query 502'd with "buffer pool is full").
	hits, err := searchHits("pool regression", "", "", 3, "")
	if err != nil {
		t.Fatalf("search after concurrent load failed: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("search after concurrent load returned no hits")
	}
	h := HTTP{}
	if _, err := h.Stats(nil); err != nil {
		t.Fatalf("stats after concurrent load failed: %v", err)
	}
}