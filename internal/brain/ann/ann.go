package ann

import (
	"bufio"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync"

	"github.com/coder/hnsw"
)

// Row is one leaf vector to index, keyed by the brain leaf id (string).
type Row struct {
	ID  string
	Vec []float32
}

// Result is one nearest-neighbor hit with a cosine similarity in [0,1]
// (higher is closer), matching the baseline linear scan's Score semantics.
type Result struct {
	ID    string
	Score float32
}

// Params are HNSW construction/query parameters (mirror of config
// vector.ann). Zero fields fall back to sane defaults for text embeddings.
type Params struct {
	Dim            int     // vector dimension (256 for the brain model)
	M              int     // max neighbors per node (16)
	Ml             float64 // layer probability factor (0.25)
	EfConstruction int     // ef used while adding (build quality, 200)
	EfSearch       int     // ef used while searching (query quality, 200)
	RngSeed        int64   // deterministic builds for tests (0 = time-seeded)
}

// Defaults returns Params with the brain defaults. M=32 (not 16): the
// empirical gate tests (offline recall@5 >= 0.95 on synthetic data, issue
// #204) only pass from M=32 up with this HNSW implementation; M=16 caps
// recall around 0.8. efSearch >= efConstruction because coder/hnsw keeps a
// single ef for build and query.
func Defaults() Params {
	return Params{Dim: 256, M: 32, Ml: 0.25, EfConstruction: 200, EfSearch: 400}
}

func (p Params) withDefaults() Params {
	d := Defaults()
	if p.Dim <= 0 {
		p.Dim = d.Dim
	}
	if p.M <= 0 {
		p.M = d.M
	}
	if p.Ml <= 0 {
		p.Ml = d.Ml
	}
	if p.EfConstruction <= 0 {
		p.EfConstruction = d.EfConstruction
	}
	if p.EfSearch <= 0 {
		p.EfSearch = d.EfSearch
	}
	// coder/hnsw uses a single ef for construction and search; keep the
	// higher of the two so build quality and query recall both hold.
	if p.EfSearch < p.EfConstruction {
		p.EfSearch = p.EfConstruction
	}
	return p
}

// WithDefaults returns p with zero fields filled from Defaults. Exported so
// callers can log/forward the effective parameters.
func (p Params) WithDefaults() Params { return p.withDefaults() }

// zeroNorm reports whether the vector is all zeros (a degenerate embedding).
// Cosine of a zero vector is NaN and must never enter the graph.
func zeroNorm(v []float32) bool {
	for _, f := range v {
		if f != 0 {
			return false
		}
	}
	return true
}

// Index is an in-memory HNSW graph over leaf embeddings with optional file
// persistence: a base snapshot (path) plus an append-only WAL (path+".wal")
// of upserts since the snapshot. Load replays the WAL; Upsert appends to it
// and mutates the graph; Save writes a fresh snapshot and resets the WAL.
//
// The graph lives entirely outside liblbug (#204): building and searching it
// never touches kb.lbug, so a liblbug crash cannot take the index down.
//
// Searches are safe for concurrent readers; Upsert/Build/Save take the write
// lock (single-writer, same rule as the brain DB).
type Index struct {
	graph *hnsw.Graph[string]
	path  string
	wal   string

	params Params
	mu     sync.RWMutex

	walFile *os.File
	// dirty counts WAL entries appended since the last snapshot (stats).
	dirty int
	// skipped counts rows refused at build/upsert (zero-norm / wrong dim).
	skipped int
}

// New returns an in-memory index (no persistence).
func New(p Params) *Index { return newIndex("", p) }

// Open loads the snapshot at path (if present) and replays the WAL. A missing
// index file is not an error: it yields an empty index, so callers fall back
// to a linear scan until a build lands. A corrupt snapshot is an error.
func Open(path string, p Params) (*Index, error) {
	idx := newIndex(path, p)
	if err := idx.loadSnapshot(); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return nil, err
		}
		// Missing snapshot: an empty index is a valid state.
	}
	if err := idx.replayWAL(); err != nil {
		return nil, err
	}
	return idx, nil
}

func newIndex(path string, p Params) *Index {
	p = p.withDefaults()
	g := hnsw.NewGraph[string]()
	g.M = p.M
	g.Ml = p.Ml
	g.EfSearch = p.EfSearch
	g.Distance = hnsw.CosineDistance
	if p.RngSeed != 0 {
		g.Rng = rand.New(rand.NewSource(p.RngSeed))
	}
	wal := path + ".wal"
	return &Index{graph: g, path: path, wal: wal, params: p}
}

// loadSnapshot imports the base graph from path (empty graph when absent).
func (ix *Index) loadSnapshot() error {
	f, err := os.Open(ix.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	g := hnsw.NewGraph[string]()
	g.M = ix.params.M
	g.Ml = ix.params.Ml
	g.EfSearch = ix.params.EfSearch
	g.Distance = hnsw.CosineDistance
	// Import needs an io.ByteReader (varint keys); os.File is not one.
	if err := g.Import(bufio.NewReader(f)); err != nil {
		return err
	}
	ix.graph = g
	return nil
}

// replayWAL appends every WAL entry to the in-memory graph. A torn tail
// (crash mid-append) is tolerated: entries up to the first bad line stick.
func (ix *Index) replayWAL() error {
	f, err := os.Open(ix.wal)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	dec := newWALDecoder(f)
	for {
		row, err := dec.Next()
		if err != nil {
			return nil // EOF or torn tail: keep the valid prefix
		}
		if ix.rowEligible(row) {
			ix.graph.Add(hnsw.MakeNode(row.ID, row.Vec))
			ix.dirty++
		} else {
			ix.skipped++
		}
	}
}

// rowEligible checks dim and non-zero norm without touching the graph.
func (ix *Index) rowEligible(r Row) bool {
	if len(r.Vec) != ix.params.Dim {
		return false
	}
	return !zeroNorm(r.Vec)
}

// Build replaces the graph with rows and, when path is set, writes a fresh
// snapshot and truncates the WAL. Duplicate ids collapse (HNSW keys are a
// map): re-building the same corpus is idempotent.
func (ix *Index) Build(rows []Row) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.graph = newGraph(ix.params)
	ix.skipped = 0
	for _, r := range rows {
		if !ix.rowEligible(r) {
			ix.skipped++
			continue
		}
		ix.graph.Add(hnsw.MakeNode(r.ID, r.Vec))
	}
	ix.dirty = 0
	if ix.path != "" {
		if err := ix.saveSnapshot(); err != nil {
			return err
		}
	}
	return nil
}

func newGraph(p Params) *hnsw.Graph[string] {
	g := hnsw.NewGraph[string]()
	g.M = p.M
	g.Ml = p.Ml
	g.EfSearch = p.EfSearch
	g.Distance = hnsw.CosineDistance
	if p.RngSeed != 0 {
		g.Rng = rand.New(rand.NewSource(p.RngSeed))
	}
	return g
}

// Upsert adds only rows whose id is not already indexed (incremental wave:
// new leafs only, never a rebuild). Re-adding an existing id is a no-op, so
// upserts are idempotent. When path is set, new rows are also appended to the
// WAL so a restart replays them.
func (ix *Index) Upsert(rows []Row) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return ix.upsertLocked(rows)
}

func (ix *Index) upsertLocked(rows []Row) error {
	if len(rows) == 0 {
		return nil
	}
	var enc *WALEncoder
	if ix.path != "" {
		if err := ix.ensureWAL(); err != nil {
			return err
		}
		enc = newWALEncoder(ix.walFile)
	}
	added := 0
	for _, r := range rows {
		if !ix.rowEligible(r) {
			ix.skipped++
			continue
		}
		if _, exists := ix.graph.Lookup(r.ID); exists {
			continue // idempotent: already indexed
		}
		ix.graph.Add(hnsw.MakeNode(r.ID, r.Vec))
		ix.dirty++
		added++
		if enc != nil {
			if err := enc.Append(r); err != nil {
				return err
			}
		}
	}
	return nil
}

// Search returns the k nearest cosine neighbors (descending similarity).
// A zero query vector or an empty index yields no hits (the caller falls
// back to a linear scan / FTS-only ranking).
func (ix *Index) Search(vec []float32, k int) []Result {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if k <= 0 || ix.graph.Len() == 0 || zeroNorm(vec) || len(vec) != ix.params.Dim {
		return nil
	}
	res := ix.graph.Search(vec, k)
	out := make([]Result, 0, len(res))
	for _, n := range res {
		out = append(out, Result{ID: n.Key, Score: cosine(vec, n.Value)})
	}
	return out
}

// Lookup reports whether id is already indexed (used for idempotent upsert).
func (ix *Index) Lookup(id string) bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	_, ok := ix.graph.Lookup(id)
	return ok
}

// Len is the number of indexed vectors.
func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.graph.Len()
}

// Dirty is the number of WAL entries since the last snapshot.
func (ix *Index) Dirty() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.dirty
}

// Skipped is the number of rows refused at build/upsert (zero-norm/dim).
func (ix *Index) Skipped() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.skipped
}

// Save writes the current graph as the new snapshot and truncates the WAL
// (compaction). No-op for in-memory indexes.
func (ix *Index) Save() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.path == "" {
		return nil
	}
	if err := ix.saveSnapshot(); err != nil {
		return err
	}
	return ix.resetWAL()
}

// SaveTo persists the current graph as a fresh snapshot at path and switches
// this index's persistence target there (subsequent Upserts append to
// path+".wal"). Used by the build tool to land a new index.
func (ix *Index) SaveTo(path string) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.path = path
	ix.wal = path + ".wal"
	if err := ix.saveSnapshot(); err != nil {
		return err
	}
	return ix.resetWAL()
}

func (ix *Index) saveSnapshot() error {
	if err := os.MkdirAll(filepath.Dir(ix.path), 0o755); err != nil {
		return err
	}
	tmp := ix.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := ix.graph.Export(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, ix.path)
}

func (ix *Index) ensureWAL() error {
	if ix.walFile != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(ix.wal), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(ix.wal, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	ix.walFile = f
	return nil
}

// resetWAL truncates the WAL after a snapshot (compaction).
func (ix *Index) resetWAL() error {
	if ix.walFile != nil {
		if err := ix.walFile.Close(); err != nil {
			return err
		}
		ix.walFile = nil
	}
	if err := os.Remove(ix.wal); err != nil && !os.IsNotExist(err) {
		return err
	}
	ix.dirty = 0
	return nil
}

// Close flushes the WAL handle. The index stays usable (in-memory).
func (ix *Index) Close() error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.walFile != nil {
		err := ix.walFile.Close()
		ix.walFile = nil
		return err
	}
	return nil
}

// ExactRank brute-forces the k nearest rows by cosine (test oracle and the
// fallback the search path uses when the ANN index is absent).
func ExactRank(q []float32, rows []Row, k int) []Result {
	type scored struct {
		r   Row
		cos float32
	}
	all := make([]scored, 0, len(rows))
	for _, r := range rows {
		if len(r.Vec) != len(q) || zeroNorm(r.Vec) {
			continue
		}
		all = append(all, scored{r: r, cos: cosine(q, r.Vec)})
	}
	// insertion sort on small result sets, stable enough for tests
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].cos > all[j-1].cos; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	if len(all) > k {
		all = all[:k]
	}
	out := make([]Result, 0, len(all))
	for _, s := range all {
		out = append(out, Result{ID: s.r.ID, Score: s.cos})
	}
	return out
}

func cosine(a, b []float32) float32 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
