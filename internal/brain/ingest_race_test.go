//go:build cgo && system_ladybug

package brain

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/eSlider/2dph/internal/brain/rank"
)

// TestIngestWriteRestoresServeConnOnError proves finding 1: when a
// write-stage step (InitSchema/AddLeafs/EnsureIndexes) fails, the serve read
// connection must still be restored on exit. Before the fix the deferred
// closeW() closed the writable handle but no refreshBrain ran, so the global
// conn stayed nil and /search, /get and /stats returned "brain not open" for
// the rest of the process lifetime.
func TestIngestWriteRestoresServeConnOnError(t *testing.T) {
	t.Setenv("KB_BUFFER_POOL", "134217728")
	t.Setenv("KB_ROOT", t.TempDir()) // dbPath() lands in temp, never the real var/
	dbpath := dbPath()
	// openWithSandbox opens an existing db (serve reads a pre-built kb.lbug),
	// so make the var/ dir the file must live in.
	if err := os.MkdirAll(filepath.Dir(dbpath), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := openWithSandbox(eps()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		brainMu.Lock()
		closeBrainLocked()
		brainMu.Unlock()
	}()

	// Force a write-stage error: AddLeafs rejects an empty source.
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
	if _, statErr := h.Stats(nil); statErr != nil {
		t.Fatalf("stats failed after restore: %v", statErr)
	}
}

// TestIngestSwapRaceSafe proves finding 2: readers (Get/Stats/Audit and the
// FTS path) hold brainMu.RLock for the whole query while Ingest's close →
// write → reopen window holds brainMu.Lock. Run with -race: before the
// RWMutex this was a data race on the package-global conn (a handler could
// execute a prepared statement against a just-closed *lbug.Connection).
func TestIngestSwapRaceSafe(t *testing.T) {
	t.Setenv("KB_BUFFER_POOL", "134217728")
	t.Setenv("KB_ROOT", t.TempDir())
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
	ids, err := AddLeafs(wconn, []LeafInput{
		{Text: "race proof leaf one", Source: "race-test", Root: "info", Type: "reference"},
		{Text: "race proof leaf two", Source: "race-test", Root: "info", Type: "reference"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexes(wconn); err != nil {
		t.Fatal(err)
	}
	wconn.Close()
	wdb.Close()

	if err := openWithSandbox(eps()); err != nil {
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
			if err := openWithSandboxLocked(eps()); err != nil {
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
				_, _ = h.Get(nil, ids[j%len(ids)], false)
				_, _ = h.Stats(nil)
				_, _ = h.Audit(nil)
				brainMu.RLock()
				stmt, err := conn.Prepare(rank.FTSStmt)
				if err == nil {
					res, _ := conn.Execute(stmt, map[string]any{"q": "race", "n": 5})
					stmt.Close()
					if res != nil {
						res.Close()
					}
				}
				brainMu.RUnlock()
			}
		}()
	}
	wg.Wait()
}
