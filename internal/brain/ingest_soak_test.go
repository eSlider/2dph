//go:build cgo && system_ladybug

package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/eSlider/2dph/internal/config"
)

// soakIngestIterations is the number of full /ingest write cycles (#109 asks
// for 100-500). Each cycle runs the real production path: parse, embed,
// OpenWritable on the live file, InitSchema/AddLeafs/EnsureIndexes, close,
// reopen the serve read handle.
const soakIngestIterations = 200

// soakReaders is the number of concurrent reader goroutines hammering the
// serve read handle while the writer swaps it underneath them.
const soakReaders = 8

// soakSetup isolates the KB under a temp root (never var/) and returns the
// previous typed config so the caller can restore it.
func soakSetup(t *testing.T) (restore func()) {
	t.Helper()
	prev := brainCfg()
	cfg := config.Defaults()
	cfg.Root = t.TempDir()
	cfg.BufferPool = 134217728 // 128MB: CI-runner friendly
	Configure(&cfg)
	return func() { Configure(&prev) }
}

// stubEmbeddings replaces the HF-model embed step with a deterministic
// offline filler so Ingest runs without network, model dir or daemon.
func stubEmbeddings(t *testing.T) {
	t.Helper()
	prev := embedIngestLeafs
	embedIngestLeafs = func(leafs []LeafInput) error {
		for i := range leafs {
			if len(leafs[i].Embedding) > 0 {
				continue
			}
			vec := make([]float64, EmbedDim)
			for d := range vec {
				vec[d] = float64((d*7+i*13)%11)/11.0 + 0.01
			}
			leafs[i].Embedding = vec
		}
		return nil
	}
	t.Cleanup(func() { embedIngestLeafs = prev })
}

// soakSeedDB builds a pre-built corpus (schema + leafs + FTS/HNSW indexes)
// the way production serves over an existing kb.lbug, returning two seed ids.
func soakSeedDB(t *testing.T) (idA, idB string) {
	t.Helper()
	wdb, wconn, err := OpenWritable(dbPath())
	if err != nil {
		t.Fatal(err)
	}
	defer wdb.Close()
	defer wconn.Close()
	if err := InitSchema(wconn); err != nil {
		t.Fatal(err)
	}
	vec := make([]float64, EmbedDim)
	vec[0] = 0.5
	ids, err := AddLeafs(wconn, []LeafInput{
		{Text: "soak seed leaf alpha", Source: "soak-seed", Root: "info", Type: "reference", Embedding: vec},
		{Text: "soak seed leaf beta", Source: "soak-seed", Root: "info", Type: "reference", Embedding: vec},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexes(wconn); err != nil {
		t.Fatal(err)
	}
	return ids[0], ids[1]
}

func soakPayload(text, source string) []byte {
	b, _ := json.Marshal(map[string]any{
		"text": text, "source": source, "root": "info", "type": "reference",
	})
	return b
}

// TestIngestSoakWritePath is the #109 deliverable: loop the write path many
// iterations while concurrent readers use the long-lived serve handle,
// asserting no crash and that reads keep working after every cycle.
//
// Before the fix this reproduces the silent Ladybug death end to end:
// Ingest opened a second handle on the live file while serve's read handle
// stayed open (same-file double-open kills liblbug in-process), and the
// close→reopen swap ran unsynchronized against readers (C use-after-close).
// Under -race the unsynchronized global conn/db access is flagged outright.
func TestIngestSoakWritePath(t *testing.T) {
	restoreCfg := soakSetup(t)
	defer restoreCfg()
	stubEmbeddings(t)

	idA, _ := soakSeedDB(t)
	if err := openWithSandbox(brainCfg().Eps); err != nil {
		t.Fatal(err)
	}
	defer closeBrain()

	h := HTTP{}
	ctx := context.Background()

	writerDone := make(chan struct{})
	var writeErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() { // writer: the real /ingest path
		defer wg.Done()
		defer close(writerDone)
		for i := 0; i < soakIngestIterations; i++ {
			out, err := h.Ingest(ctx, soakPayload(fmt.Sprintf("soak leaf number %d zebra", i), "soak-write"))
			if err != nil {
				writeErr = fmt.Errorf("ingest %d: %w", i, err)
				return
			}
			var resp map[string]any
			if err := json.Unmarshal(out, &resp); err != nil {
				writeErr = fmt.Errorf("ingest %d decode: %w", i, err)
				return
			}
		}
	}()

	for r := 0; r < soakReaders; r++ { // readers: what serve handlers execute
		wg.Add(1)
		go func() {
			defer wg.Done()
			vec := make([]float64, EmbedDim)
			vec[1] = 0.25
			n := 0
			for {
				select {
				case <-writerDone:
					return
				default:
				}
				if _, err := h.Stats(ctx); err != nil {
					t.Errorf("stats: %v", err)
					return
				}
				if _, err := h.Audit(ctx); err != nil {
					t.Errorf("audit: %v", err)
					return
				}
				if _, err := h.Get(ctx, idA, false); err != nil {
					t.Errorf("get: %v", err)
					return
				}
				if _, err := queryFTS("zebra", 5); err != nil {
					t.Errorf("fts: %v", err)
					return
				}
				if _, err := queryVector(vec, 5); err != nil {
					t.Errorf("vector: %v", err)
					return
				}
				n++
				if n >= 10000 {
					return
				}
			}
		}()
	}

	wg.Wait()
	if writeErr != nil {
		t.Fatal(writeErr)
	}

	// The serve handle must survive every swap and still see all writes.
	if conn == nil {
		t.Fatal("serve conn nil after soak")
	}
	if _, err := h.Stats(ctx); err != nil {
		t.Fatalf("stats failed after soak: %v", err)
	}
	hits, err := queryFTS("zebra", soakIngestIterations+10)
	if err != nil {
		t.Fatalf("fts failed after soak: %v", err)
	}
	if len(hits) < soakIngestIterations {
		t.Fatalf("fts found %d soaked leafs, want >= %d", len(hits), soakIngestIterations)
	}
}
