//go:build cgo && system_ladybug

// ANN query-vector integration tests (issue #204): the queryVector ANN path,
// its fallback to the brute-force scan, and the A/B recall parity on a
// synthetic fixture DB. All offline: deterministic embeddings, no model, no
// network.
package brain

import (
	"math/rand"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/brain/ann"
)

func itoa(i int) string { return strconv.Itoa(i) }

// annFixtureLeafs returns n synthetic leafs whose embeddings form nClusters
// tight clusters (deterministic, model-free) plus one query per cluster.
func annFixtureLeafs(n, nClusters int) []LeafInput {
	rng := rand.New(rand.NewSource(204))
	const dim = EmbedDim
	centroids := make([][]float64, nClusters)
	for c := range centroids {
		v := make([]float64, dim)
		for i := range v {
			v[i] = rng.Float64()*2 - 1
		}
		norm := 0.0
		for _, f := range v {
			norm += f * f
		}
		for i := range v {
			v[i] /= sqrt64(norm)
		}
		centroids[c] = v
	}
	leafs := make([]LeafInput, n)
	for i := range leafs {
		c := i % nClusters
		emb := make([]float64, dim)
		copy(emb, centroids[c])
		for j := 0; j < dim; j += 8 {
			emb[j] += (rng.Float64() - 0.5) * 0.02
		}
		leafs[i] = LeafInput{
			Text:      "ANN fixture " + itoa(i) + " cluster " + itoa(c),
			Source:    "ann-fixture.md",
			Root:      "info",
			Type:      "reference",
			How:       "ann-gate-test",
			Embedding: emb,
		}
	}
	return leafs
}

// annFixtureDB builds a scratch kb.lbug with the fixture leafs and returns
// the db path. The read handle is left closed (openBrain happens in tests).
func annFixtureDB(t *testing.T, n, nClusters int) string {
	t.Helper()
	return annFixtureDBLeafs(t, annFixtureLeafs(n, nClusters))
}

// annFixtureDBLeafs writes arbitrary leafs into a scratch kb.lbug.
func annFixtureDBLeafs(t *testing.T, leafs []LeafInput) string {
	t.Helper()
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kb.lbug")
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := AddLeafs(conn, leafs); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexes(conn); err != nil {
		t.Fatal(err)
	}
	db.Close()
	conn.Close()
	return dbpath
}

// annFixtureIndex builds an ANN index over the fixture vectors (mirror of
// what bin/brain/ann.go build does, minus the DB extraction step).
func annFixtureIndex(t *testing.T, leafs []LeafInput, path string) *ann.Index {
	t.Helper()
	rows := make([]ann.Row, 0, len(leafs))
	for i, lf := range leafs {
		rows = append(rows, ann.Row{ID: itoa(i), Vec: toFloat32(lf.Embedding, EmbedDim)})
	}
	idx, err := ann.Open(path, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Build(rows); err != nil {
		t.Fatal(err)
	}
	if err := idx.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	return idx
}

// openFixtureRead points the read path at dbpath and opens the read handle.
func openFixtureRead(t *testing.T, dbpath string) {
	t.Helper()
	prev := dbPathFn
	dbPathFn = func() string { return dbpath }
	t.Cleanup(func() { dbPathFn = prev })
	if err := openBrain(); err != nil {
		t.Fatalf("openBrain fixture: %v", err)
	}
	t.Cleanup(closeBrain)
}

// setANNIndexPath points the ANN index at path and drops the process-cached
// handle so the next annIndex() reopens it. Restored on cleanup.
func setANNIndexPath(t *testing.T, path string) {
	t.Helper()
	prevCfg := activeCfg.Vector.ANN.Index
	activeCfg.Vector.ANN.Index = path
	annMu.Lock()
	if annIdx != nil {
		annIdx.Close()
	}
	annIdx, annOpen = nil, nil
	annStats = annInfo{}
	annMu.Unlock()
	t.Cleanup(func() {
		activeCfg.Vector.ANN.Index = prevCfg
		annMu.Lock()
		if annIdx != nil {
			annIdx.Close()
		}
		annIdx, annOpen = nil, nil
		annMu.Unlock()
	})
}

func TestANNQueryVectorUsesIndex(t *testing.T) {
	const n, nClusters = 300, 10
	dbpath := annFixtureDB(t, n, nClusters)
	openFixtureRead(t, dbpath)

	rows, err := extractRows(0)
	if err != nil {
		t.Fatalf("extractRows: %v", err)
	}
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	idx, err := ann.Open(idxPath, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Build(rows); err != nil {
		t.Fatal(err)
	}
	if err := idx.SaveTo(idxPath); err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	setANNIndexPath(t, idxPath)
	setAnnMode(annForceOn)
	defer setAnnMode(annAuto)

	// A query near cluster 1 must return hits through the ANN path, with
	// metadata loaded from the graph (text/root populated).
	leafs := annFixtureLeafs(n, nClusters)
	hits, err := annQueryVector(leafs[1].Embedding, 5)
	if err != nil {
		t.Fatalf("annQueryVector: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("annQueryVector returned no hits through the index")
	}
	if hits[0].Text == "" || hits[0].Root != "info" {
		t.Fatalf("ANN hit missing metadata: %+v", hits[0])
	}
}

func TestANNQueryVectorFallbackOnMissingIndex(t *testing.T) {
	dbpath := annFixtureDB(t, 200, 8)
	// No index file exists next to the fixture db.
	openFixtureRead(t, dbpath)
	setAnnMode(annForceOn)
	defer setAnnMode(annAuto)

	// annQueryVector alone must yield nothing (empty index → fallback);
	// queryVector must still return hits via the brute-force scan.
	leafs := annFixtureLeafs(200, 8)
	hits, err := annQueryVector(leafs[3].Embedding, 5)
	if err != nil {
		t.Fatalf("annQueryVector: %v", err)
	}
	if len(hits) != 0 {
		t.Fatal("annQueryVector without an index must return no hits")
	}
	all, err := queryVector(leafs[3].Embedding, 5)
	if err != nil {
		t.Fatalf("queryVector: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("fallback scan must still return hits without an index")
	}
}

func TestANNIncrementalUpsertProof(t *testing.T) {
	// Incrementality acceptance (#204): +100 new leafs must NOT require a
	// rebuild — the upsert appends only new vectors (WAL), completes in
	// well under a second, and the replayed index finds the new leafs.
	const baseN, extraN = 500, 100
	dbpath := annFixtureDB(t, baseN, 10)
	openFixtureRead(t, dbpath)

	rows, err := extractRows(0)
	if err != nil {
		t.Fatalf("extractRows: %v", err)
	}
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	idx, err := ann.Open(idxPath, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Build(rows); err != nil {
		t.Fatal(err)
	}
	if err := idx.SaveTo(idxPath); err != nil {
		t.Fatal(err)
	}
	if idx.Dirty() != 0 {
		t.Fatalf("dirty after build = %d, want 0", idx.Dirty())
	}

	// Add 100 new leafs through the normal write path.
	extra := annFixtureLeafs(extraN, 10)
	for i := range extra {
		extra[i].Text = "ANN fixture new " + itoa(i) + ": incremental wave leaf"
	}
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddLeafs(conn, extra); err != nil {
		t.Fatal(err)
	}
	db.Close()
	conn.Close()

	// The upsert flow (mirror of bin/brain/ann.go upsert): extract → filter
	// by Lookup → Upsert (append WAL). Measure the add phase itself.
	newRows := make([]ann.Row, 0, extraN)
	for _, lf := range extra {
		id := LeafID(lf.Text, lf.Source)
		if !idx.Lookup(id) {
			newRows = append(newRows, ann.Row{ID: id, Vec: toFloat32(lf.Embedding, EmbedDim)})
		}
	}
	if len(newRows) != extraN {
		t.Fatalf("filtered %d new rows, want %d", len(newRows), extraN)
	}
	start := time.Now()
	if err := idx.Upsert(newRows); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if idx.Len() != baseN+extraN {
		t.Fatalf("len after upsert = %d, want %d", idx.Len(), baseN+extraN)
	}
	if idx.Dirty() != extraN {
		t.Fatalf("dirty = %d, want %d (only new vectors in WAL)", idx.Dirty(), extraN)
	}
	if elapsed > time.Second {
		t.Fatalf("upsert of %d vectors took %s > 1s (incremental must be fast)", extraN, elapsed)
	}
	t.Logf("incremental upsert: +%d vectors in %s (no rebuild), WAL=%d", extraN, elapsed, idx.Dirty())

	// Reopen: the WAL replays the new vectors without a rebuild.
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	idx2, err := ann.Open(idxPath, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	defer idx2.Close()
	if idx2.Len() != baseN+extraN {
		t.Fatalf("replayed len = %d, want %d", idx2.Len(), baseN+extraN)
	}
	for _, r := range newRows {
		if !idx2.Lookup(r.ID) {
			t.Fatalf("WAL-replayed id %s missing after reopen", r.ID)
		}
	}
}

func TestANNRecallParityWithScan(t *testing.T) {
	// The A/B gate: ANN top-5 must agree with the exact scan top-5 on the
	// fixture corpus (recall@5 >= 0.95). Rows come from the real DB
	// extraction path (leaf ids are content hashes, not sequence numbers).
	const n, nClusters = 600, 12
	dbpath := annFixtureDB(t, n, nClusters)
	openFixtureRead(t, dbpath)

	rows, err := extractRows(0)
	if err != nil {
		t.Fatalf("extractRows: %v", err)
	}
	if len(rows) != n {
		t.Fatalf("extracted %d rows, want %d", len(rows), n)
	}
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	idx, err := ann.Open(idxPath, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Build(rows); err != nil {
		t.Fatal(err)
	}
	if err := idx.SaveTo(idxPath); err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	setANNIndexPath(t, idxPath)

	rng := rand.New(rand.NewSource(1))
	recalled, total := 0, 0
	leafs := annFixtureLeafs(n, nClusters)
	for c := 0; c < nClusters; c++ {
		q := make([]float64, EmbedDim)
		copy(q, leafs[c].Embedding)
		for j := 0; j < EmbedDim; j += 8 {
			q[j] += (rng.Float64() - 0.5) * 0.005
		}
		truth := ann.ExactRank(toFloat32(q, EmbedDim), rows, 5)
		setAnnMode(annForceOn)
		annHits, err := queryVector(q, 5)
		if err != nil {
			t.Fatal(err)
		}
		want := make(map[string]bool, 5)
		for _, tr := range truth {
			want[tr.ID] = true
		}
		hit := 0
		for _, h := range annHits {
			if want[h.ID] {
				hit++
			}
		}
		recalled += hit
		total += 5
	}
	setAnnMode(annAuto)
	score := float64(recalled) / float64(total)
	t.Logf("ANN vs scan recall@5 = %.3f (%d/%d)", score, recalled, total)
	if score < 0.95 {
		t.Fatalf("ANN recall@5 = %.3f < 0.95 vs baseline scan", score)
	}
}
