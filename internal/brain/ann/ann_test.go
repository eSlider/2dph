package ann

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// syntheticRows returns n 256-dim vectors grouped into nClusters tight
// clusters (cluster vectors share a centroid plus small noise), so exact
// nearest neighbors are well-defined and the recall gate is meaningful.
func syntheticRows(n, nClusters int, seed int64) []Row {
	rng := rand.New(rand.NewSource(seed))
	const dim = 256
	centroids := make([][]float32, nClusters)
	for c := range centroids {
		v := make([]float32, dim)
		for i := range v {
			v[i] = rng.Float32()*2 - 1
		}
		norm := 0.0
		for _, f := range v {
			norm += float64(f) * float64(f)
		}
		inv := float32(1 / sqrt64(norm))
		for i := range v {
			v[i] *= inv
		}
		centroids[c] = v
	}
	rows := make([]Row, n)
	for i := range rows {
		c := i % nClusters
		v := make([]float32, dim)
		copy(v, centroids[c])
		for j := 0; j < dim; j += 8 {
			v[j] += (rng.Float32() - 0.5) * 0.02
		}
		rows[i] = Row{ID: "leaf-" + itoa(i), Vec: v}
	}
	return rows
}

// queryNear returns a vector near cluster c (a fresh noisy copy), and its
// expected nearest neighbors are the cluster's own vectors.
func queryNear(rows []Row, nClusters, c int, rng *rand.Rand) []float32 {
	q := make([]float32, 256)
	for _, r := range rows {
		if r.ID == "leaf-"+itoa(c) { // first member of the cluster as base
			copy(q, r.Vec)
			break
		}
	}
	for j := 0; j < len(q); j += 8 {
		q[j] += (rng.Float32() - 0.5) * 0.01
	}
	return q
}

func sqrt64(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 40; i++ {
		z = 0.5 * (z + x/z)
	}
	return z
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func recall5(ann []Result, truth []Result) int {
	want := make(map[string]bool, 5)
	for _, t := range truth[:5] {
		want[t.ID] = true
	}
	hit := 0
	for _, a := range ann {
		if want[a.ID] {
			hit++
		}
	}
	return hit
}

func TestANNBuildSearchRecallGate(t *testing.T) {
	// 2000 vectors in 10 clusters, default params (M=32/ef=400) — recall@5
	// must beat 0.95 against the exact brute-force ranking (the A/B gate of
	// #204). M=16 caps recall at ~0.8 on this synthetic corpus (see Defaults).
	rows := syntheticRows(2000, 10, 42)
	ix := New(Params{RngSeed: 7})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	if ix.Len() != 2000 {
		t.Fatalf("len = %d, want 2000", ix.Len())
	}
	rng := rand.New(rand.NewSource(1))
	total, hits := 0, 0
	for c := 0; c < 10; c++ {
		q := queryNear(rows, 10, c, rng)
		got := ix.Search(q, 5)
		truth := ExactRank(q, rows, 5)
		total++
		rec := recall5(got, truth)
		hits += rec
		if rec < 5 {
			t.Logf("cluster %d: recall %d/5", c, rec)
		}
	}
	if total == 0 {
		t.Fatal("no queries")
	}
	score := float64(hits) / float64(total*5)
	t.Logf("recall@5 = %.3f (%d/%d)", score, hits, total*5)
	if score < 0.95 {
		t.Fatalf("recall@5 = %.3f < 0.95", score)
	}
}

func TestANNUpsertIncrementalAndIdempotent(t *testing.T) {
	base := syntheticRows(100, 5, 11)
	ix := New(Params{RngSeed: 3})
	if err := ix.Build(base); err != nil {
		t.Fatalf("build: %v", err)
	}
	if ix.Len() != 100 {
		t.Fatalf("len = %d, want 100", ix.Len())
	}

	// Upsert 100 new vectors (ids 100..199) — the wave case: only new leafs.
	fresh := syntheticRows(100, 5, 12)
	for i := range fresh {
		fresh[i].ID = "leaf-" + itoa(100+i)
	}
	if err := ix.Upsert(fresh); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if ix.Len() != 200 {
		t.Fatalf("len after upsert = %d, want 200", ix.Len())
	}
	if ix.Dirty() != 100 {
		t.Fatalf("dirty = %d, want 100 (only new rows)", ix.Dirty())
	}

	// Idempotence: re-running the same upsert must not duplicate anything.
	if err := ix.Upsert(fresh); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	if ix.Len() != 200 {
		t.Fatalf("len after re-upsert = %d, want 200 (idempotent)", ix.Len())
	}
	if ix.Dirty() != 100 {
		t.Fatalf("dirty after re-upsert = %d, want 100", ix.Dirty())
	}

	// The new vectors are searchable after the incremental upsert.
	rng := rand.New(rand.NewSource(2))
	q := queryNear(fresh, 5, 1, rng)
	got := ix.Search(q, 5)
	if len(got) == 0 {
		t.Fatal("search after upsert returned nothing")
	}
	truth := ExactRank(q, append(append([]Row{}, base...), fresh...), 5)
	if recall5(got, truth) < 5 {
		t.Fatalf("post-upsert recall < 5/5: got %v", got)
	}
}

func TestANNSaveLoadReplayWAL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vector.ann")

	rows := syntheticRows(100, 5, 21)
	ix := New(Params{RngSeed: 5})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	// persist a snapshot (as bin/brain/ann.go build does)
	if err := ix.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	ix2, err := Open(path, Params{RngSeed: 5})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if ix2.Len() != 100 {
		t.Fatalf("reopened len = %d, want 100", ix2.Len())
	}

	// Incremental upsert after the snapshot: appended to the WAL.
	fresh := syntheticRows(10, 5, 22)
	for i := range fresh {
		fresh[i].ID = "leaf-new-" + itoa(i)
	}
	if err := ix2.Upsert(fresh); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := ix2.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen: WAL replays the 10 new rows without a rebuild.
	ix3, err := Open(path, Params{RngSeed: 5})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer ix3.Close()
	if ix3.Len() != 110 {
		t.Fatalf("replayed len = %d, want 110", ix3.Len())
	}
	if !ix3.Lookup("leaf-new-3") {
		t.Fatal("WAL-replayed id not searchable")
	}
}

func TestANNMissingFileIsEmptyFallback(t *testing.T) {
	// A missing index must not error: callers fall back to the linear scan.
	ix, err := Open(filepath.Join(t.TempDir(), "nope.ann"), Defaults())
	if err != nil {
		t.Fatalf("open missing: %v", err)
	}
	if ix.Len() != 0 {
		t.Fatalf("len = %d, want 0", ix.Len())
	}
	if ix.Search(make([]float32, 256), 5) != nil {
		t.Fatal("search on empty index must return nil (fallback)")
	}
}

func TestANNCorruptSnapshotErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.ann")
	if err := os.WriteFile(path, []byte("this is not an hnsw snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, Defaults()); err == nil {
		t.Fatal("corrupt snapshot must return an error (caller falls back)")
	}
}

func TestANNZeroVectorsSkipped(t *testing.T) {
	rows := syntheticRows(50, 5, 31)
	rows = append(rows, Row{ID: "zero-1", Vec: make([]float32, 256)})
	rows = append(rows, Row{ID: "zero-2", Vec: make([]float32, 256)})
	ix := New(Params{RngSeed: 9})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	if ix.Len() != 50 {
		t.Fatalf("len = %d, want 50 (zero vectors skipped)", ix.Len())
	}
	if ix.Skipped() != 2 {
		t.Fatalf("skipped = %d, want 2", ix.Skipped())
	}
	if ix.Lookup("zero-1") {
		t.Fatal("zero vector must not be indexed")
	}
}

func TestANNWrongDimSkipped(t *testing.T) {
	rows := syntheticRows(20, 4, 41)
	rows = append(rows, Row{ID: "short", Vec: make([]float32, 128)})
	ix := New(Params{RngSeed: 4})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	if ix.Len() != 20 {
		t.Fatalf("len = %d, want 20", ix.Len())
	}
	if ix.Skipped() != 1 {
		t.Fatalf("skipped = %d, want 1", ix.Skipped())
	}
}
