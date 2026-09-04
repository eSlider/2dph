//go:build cgo && system_ladybug

package brain

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/eSlider/2dph/internal/brain/rank"
)

// TestIngestWriteRestoresServeConnOnError proves: when a write-stage step
// (InitSchema/AddLeafs/EnsureIndexes) fails, the serve read connection must
// still be restored on exit. Before the fix the deferred closeW() closed the
// writable handle but no refreshBrain ran on error paths, leaving the serve
// handle stale (or gone), so /search, /get and /stats served an old snapshot
// or "brain not open" for the rest of the process lifetime.
func TestIngestWriteRestoresServeConnOnError(t *testing.T) {
	restoreCfg := soakSetup(t)
	defer restoreCfg()
	stubEmbeddings(t)

	dbpath := dbPath()
	if err := os.MkdirAll(filepath.Dir(dbpath), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := openWithSandbox(brainCfg().Eps); err != nil {
		t.Fatal(err)
	}
	defer func() {
		brainMu.Lock()
		closeBrainLocked()
		brainMu.Unlock()
	}()

	// Force a write-stage error: UpsertLeaf rejects an empty source.
	brainMu.Lock()
	_, err := ingestWriteLocked(dbpath, []LeafInput{{Text: "x", Source: ""}})
	brainMu.Unlock()
	if err == nil {
		t.Fatal("expected AddLeafs to reject empty source")
	}

	if conn == nil {
		t.Fatal("serve conn not restored after write-stage error")
	}
	h := HTTP{}
	if _, statErr := h.Stats(t.Context()); statErr != nil {
		t.Fatalf("stats failed after restore: %v", statErr)
	}
}

// TestIngestSwapRaceSafe proves: readers (Get/Stats/Audit and the FTS/vector
// path) hold brainMu.RLock for the whole query while Ingest's close → write →
// reopen window holds brainMu.Lock. Run with -race: before the RWMutex this
// was a data race on the package-global conn (a handler could execute a
// prepared statement against a just-closed *lbug.Connection — the C-level
// use-after-close behind the #109 segfault).
func TestIngestSwapRaceSafe(t *testing.T) {
	restoreCfg := soakSetup(t)
	defer restoreCfg()
	dbpath := dbPath()

	// Seed the db writable first (like production: an existing kb.lbug), then
	// open the long-lived serve read handle the readers will use.
	wdb, wconn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(wconn); err != nil {
		t.Fatal(err)
	}
	vec := make([]float64, EmbedDim)
	vec[0] = 0.5
	ids, err := AddLeafs(wconn, []LeafInput{
		{Text: "race proof leaf one", Source: "race-test", Root: "info", Type: "reference", Embedding: vec},
		{Text: "race proof leaf two", Source: "race-test", Root: "info", Type: "reference", Embedding: vec},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexes(wconn); err != nil {
		t.Fatal(err)
	}
	wconn.Close()
	wdb.Close()

	if err := openWithSandbox(brainCfg().Eps); err != nil {
		t.Fatal(err)
	}
	defer func() {
		brainMu.Lock()
		closeBrainLocked()
		brainMu.Unlock()
	}()

	var wg sync.WaitGroup

	// Writer mirrors Ingest's swap: close the read handle, reopen it.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 60; i++ {
			brainMu.Lock()
			closeBrainLocked()
			if err := openWithSandboxLocked(brainCfg().Eps); err != nil {
				brainMu.Unlock()
				t.Error(err)
				return
			}
			brainMu.Unlock()
		}
	}()

	// Readers hammer the RLock-guarded endpoints mid-swap.
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := HTTP{}
			for j := 0; j < 60; j++ {
				if _, err := h.Get(t.Context(), ids[j%len(ids)], false); err != nil {
					t.Errorf("get: %v", err)
					return
				}
				if _, err := h.Stats(t.Context()); err != nil {
					t.Errorf("stats: %v", err)
					return
				}
				if _, err := h.Audit(t.Context()); err != nil {
					t.Errorf("audit: %v", err)
					return
				}
				emb := make([]float64, EmbedDim)
				emb[0] = 0.5
				if _, err := queryVector(emb, 5); err != nil {
					t.Errorf("vector: %v", err)
					return
				}
				brainMu.RLock()
				stmt, err := conn.Prepare(rank.FTSStmt)
				if err == nil {
					res, execErr := conn.Execute(stmt, map[string]any{"q": "race", "n": 5})
					stmt.Close()
					if execErr == nil && res != nil {
						res.Close()
					}
				}
				brainMu.RUnlock()
			}
		}()
	}
	wg.Wait()
}
