//go:build cgo && system_ladybug

package brain

import (
	"sync"
	"testing"

	"github.com/eSlider/2dph/internal/brain/rank"
)

// TestConcurrentSearchesDontExhaustBufferPool is a regression test for the
// QueryResult leak: conn.Query/conn.Execute results that were never closed
// pinned the Ladybug buffer pool until "Buffer manager exception: Unable to
// allocate memory! The buffer pool is full" made every endpoint 502 until a
// process restart. With defer res.Close() the pool stays evictable under load.
//
// The loop deliberately avoids searchHits/embedQuery (no embed daemon or
// model on the CI runner) and drives the leak through the non-embedding
// endpoints (Stats/Audit/Get) plus a raw FTS scan that pins index pages.
func TestConcurrentSearchesDontExhaustBufferPool(t *testing.T) {
	t.Setenv("KB_BUFFER_POOL", "134217728")
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
	ids := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		got, err := AddLeafs(c, []LeafInput{{
			Text: "pool regression leaf number ", Source: "pool-test", Root: "info",
			Type: "reference", Embedding: emb,
		}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, got[0])
	}
	if err := EnsureIndexes(c); err != nil {
		t.Fatal(err)
	}

	ftsScan := func() error {
		stmt, err := c.Prepare(rank.FTSStmt)
		if err != nil {
			return err
		}
		defer stmt.Close()
		res, err := c.Execute(stmt, map[string]any{"q": "pool", "n": 5})
		if err != nil {
			return err
		}
		defer res.Close()
		for res.HasNext() {
			if _, err := res.Next(); err != nil {
				return err
			}
		}
		return nil
	}

	// Drive the leak path through the package-level conn like the live serve.
	db, conn = d, c
	defer func() { db, conn = nil, nil }()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 12; j++ {
				_ = ftsScan()
				h := HTTP{}
				_, _ = h.Stats(nil)
				_, _ = h.Audit(nil)
				_, _ = h.Get(nil, ids[j%len(ids)], false)
			}
		}()
	}
	wg.Wait()

	// The pool must still be usable: a fresh scan + stats must succeed
	// (previously every query 502'd with "buffer pool is full").
	if err := ftsScan(); err != nil {
		t.Fatalf("fts scan after concurrent load failed: %v", err)
	}
	h := HTTP{}
	if _, err := h.Stats(nil); err != nil {
		t.Fatalf("stats after concurrent load failed: %v", err)
	}
}