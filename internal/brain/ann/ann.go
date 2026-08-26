package ann

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
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

// Params are IVF parameters (mirror of config vector.ann). Zero fields fall
// back to sane defaults for text embeddings.
type Params struct {
	Dim     int // vector dimension (256 for the brain model)
	NList   int // k-means cells (default 2000 for ~300k vectors)
	NProbe  int // cells scanned per search (default 128: 6.4% of NList)
	RngSeed int64
}

// Defaults returns Params with the brain defaults. NList=2000 (cells of
// ~150 vectors at 313k), NProbe=128: measured recall@30 >= 0.93 on the real
// corpus at ~100ms/search (probe #204). coder/hnsw (HNSW) was dropped here:
// on 313k real embeddings only 34% of nodes stay reachable from the entry
// (isolated nodes, naive neighbor eviction) — recall@5 collapsed to 0.16.
func Defaults() Params {
	return Params{Dim: 256, NList: 2000, NProbe: 128}
}

func (p Params) withDefaults() Params {
	d := Defaults()
	if p.Dim <= 0 {
		p.Dim = d.Dim
	}
	if p.NList <= 0 {
		p.NList = d.NList
	}
	if p.NProbe <= 0 {
		p.NProbe = d.NProbe
	}
	return p
}

// WithDefaults returns p with zero fields filled from Defaults. Exported so
// callers can log/forward the effective parameters.
func (p Params) WithDefaults() Params { return p.withDefaults() }

// zeroNorm reports whether the vector is all zeros (a degenerate embedding).
// Cosine of a zero vector is NaN and must never enter the index.
func zeroNorm(v []float32) bool {
	for _, f := range v {
		if f != 0 {
			return false
		}
	}
	return true
}

// Index is an in-memory IVF (inverted file) over leaf embeddings with file
// persistence: a base snapshot (path) plus an append-only WAL (path+".wal")
// of upserts since the snapshot. Load replays the WAL; Upsert appends to it
// and mutates the lists; Save writes a fresh snapshot and resets the WAL.
//
// The index lives entirely outside liblbug (#204): building and searching it
// never touches kb.lbug, so a liblbug crash cannot take the index down.
// Search is exact-cosine within the NProbe probed cells — the k-means cells
// only restrict the candidate set, never re-rank it.
//
// Searches are safe for concurrent readers; Upsert/Build/Save take the write
// lock (single-writer, same rule as the brain DB).
type Index struct {
	params Params

	centroids [][]float32 // NList x Dim, L2-normalized k-means means
	lists     [][]Row     // vectors per cell
	idCell    map[string]int

	path  string
	wal   string
	mu    sync.RWMutex

	walFile *os.File
	dirty   int // WAL entries since the last snapshot
	skipped int // rows refused at build/upsert (zero-norm / wrong dim)
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
	return &Index{params: p, path: path, wal: path + ".wal", idCell: map[string]int{}}
}

// rowEligible checks dim and non-zero norm without touching the index.
func (ix *Index) rowEligible(r Row) bool {
	if len(r.Vec) != ix.params.Dim {
		return false
	}
	return !zeroNorm(r.Vec)
}

// Build replaces the index with rows and, when path is set, writes a fresh
// snapshot and truncates the WAL. Duplicate ids collapse (idCell is a map):
// re-building the same corpus is idempotent.
//
// The k-means sample is shuffled and capped at 50k so a clustered corpus
// (import waves) cannot skew the cells (see probe #204). Deterministic with
// a RngSeed, time-seeded otherwise.
func (ix *Index) Build(rows []Row) error {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.centroids, ix.lists, ix.idCell = nil, nil, map[string]int{}
	ix.skipped = 0

	eligible := make([]Row, 0, len(rows))
	for _, r := range rows {
		if !ix.rowEligible(r) {
			ix.skipped++
			continue
		}
		eligible = append(eligible, r)
	}
	if len(eligible) == 0 {
		ix.dirty = 0
		if ix.path != "" {
			if err := ix.saveSnapshot(); err != nil {
				return err
			}
		}
		return nil
	}

	sample := ix.sample(eligible, 50000)
	// Cap cells for small corpora: k-means with k ~ n is slow and pointless
	// (cells of ~1-4 vectors). At 313k the cap never fires.
	nl := ix.params.NList
	if cap := len(eligible) / 4; cap < nl {
		if cap < 32 {
			cap = 32
		}
		nl = cap
	}
	ix.centroids = kmeans(sample, nl, 10, ix.params.RngSeed)
	ix.assign(eligible)

	ix.dirty = 0
	if ix.path != "" {
		if err := ix.saveSnapshot(); err != nil {
			return err
		}
	}
	return nil
}

// sample returns a shuffled copy of rows capped at maxN (k-means input).
func (ix *Index) sample(rows []Row, maxN int) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	seed := ix.params.RngSeed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	rand.New(rand.NewSource(seed)).Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	if len(out) > maxN {
		out = out[:maxN]
	}
	return out
}

// assign puts every eligible row into the nearest cell and fills idCell.
func (ix *Index) assign(rows []Row) {
	nl := len(ix.centroids)
	cells := make([][]Row, nl)
	// bounded workers: cosine to NList centroids per row, parallel.
	workers := 8
	if len(rows) < workers {
		workers = len(rows)
	}
	if workers < 1 {
		workers = 1
	}
	type job struct {
		idx int
		r   Row
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	var cellsMu sync.Mutex
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for j := range jobs {
				c := nearestCell(j.r.Vec, ix.centroids)
				cellsMu.Lock()
				cells[c] = append(cells[c], j.r)
				cellsMu.Unlock()
			}
		}()
	}
	for i, r := range rows {
		jobs <- job{idx: i, r: r}
	}
	close(jobs)
	wg.Wait()
	ix.lists = cells
	ix.idCell = make(map[string]int, len(rows))
	for c, cell := range cells {
		for _, r := range cell {
			ix.idCell[r.ID] = c
		}
	}
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
		if _, exists := ix.idCell[r.ID]; exists {
			continue // idempotent: already indexed
		}
		if len(ix.centroids) == 0 {
			// Index built empty (no eligible rows): bootstrap a single cell.
			ix.centroids = [][]float32{r.Vec}
			ix.lists = make([][]Row, 1)
		}
		c := nearestCell(r.Vec, ix.centroids)
		ix.lists[c] = append(ix.lists[c], r)
		ix.idCell[r.ID] = c
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

// Search returns the k nearest cosine neighbors (descending similarity)
// among the vectors of the NProbe cells whose centroids are closest to the
// query. A zero query vector or an empty index yields no hits (the caller
// falls back to a linear scan / FTS-only ranking).
func (ix *Index) Search(vec []float32, k int) []Result {
	return ix.search(float64Vec(vec), k)
}

// Search64 is Search for the float64 query vector the embedding daemon
// returns. The cosine is accumulated in float64 over the float32 stored
// values — bit-identical to the baseline linear scan's cosineToQuery — so
// the candidate order matches the scan exactly when the probed cells cover
// the true neighbors (probe #204: float32 query rounding reordered dense
// top-30 and broke recall@5 vs baseline).
func (ix *Index) Search64(q []float64, k int) []Result {
	return ix.search(q, k)
}

func (ix *Index) search(q []float64, k int) []Result {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	if k <= 0 || len(ix.centroids) == 0 || zeroNorm64(q) {
		return nil
	}
	// Score the cells (exact cosine to NList centroids).
	type cs struct {
		idx int
		sim float64
	}
	cells := make([]cs, len(ix.centroids))
	for i, c := range ix.centroids {
		cells[i] = cs{idx: i, sim: cosine64(q, c)}
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].sim > cells[j].sim })
	nprobe := ix.params.NProbe
	if nprobe > len(cells) {
		nprobe = len(cells)
	}
	// Collect candidates from the probed cells and rank by exact cosine.
	type cand struct {
		r   Row
		sim float64
	}
	var out []cand
	for p := 0; p < nprobe; p++ {
		for _, r := range ix.lists[cells[p].idx] {
			out = append(out, cand{r: r, sim: cosine64(q, r.Vec)})
		}
	}
	// Deterministic ranking: descending similarity, ties broken by id —
	// identical to the baseline scan's queryVector tie-break, so the RRF
	// merge yields the same top-k when the candidate sets match (#204).
	sort.Slice(out, func(i, j int) bool {
		if out[i].sim != out[j].sim {
			return out[i].sim > out[j].sim
		}
		return out[i].r.ID < out[j].r.ID
	})
	if len(out) > k {
		out = out[:k]
	}
	res := make([]Result, len(out))
	for i, c := range out {
		res[i] = Result{ID: c.r.ID, Score: float32(c.sim)}
	}
	return res
}

// Lookup reports whether id is already indexed (used for idempotent upsert).
func (ix *Index) Lookup(id string) bool {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	_, ok := ix.idCell[id]
	return ok
}

// Len is the number of indexed vectors.
func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.idCell)
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

// Save writes the current index as the new snapshot and truncates the WAL
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

// SaveTo persists the current index as a fresh snapshot at path and switches
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

// snapshot format (little-endian, fixed layout so 320MB loads fast):
//
//	magic "2DPHANN1" (8 bytes)
//	dim int32, nlist int32, nprobe int32
//	centroids: nlist * (dim * float32)
//	cells: nlist * { count int32, count * (idLen int32, id, dim * float32) }
const snapshotMagic = "2DPHANN1"

func (ix *Index) saveSnapshot() error {
	if err := os.MkdirAll(filepath.Dir(ix.path), 0o755); err != nil {
		return err
	}
	tmp := ix.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	if err := ix.writeSnapshot(bw); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := bw.Flush(); err != nil {
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

func (ix *Index) writeSnapshot(w io.Writer) error {
	if _, err := w.Write([]byte(snapshotMagic)); err != nil {
		return err
	}
	p := ix.params
	hdr := [3]int32{int32(p.Dim), int32(len(ix.centroids)), int32(p.NProbe)}
	if err := binary.Write(w, binary.LittleEndian, &hdr); err != nil {
		return err
	}
	for _, c := range ix.centroids {
		for _, v := range c {
			if err := binary.Write(w, binary.LittleEndian, v); err != nil {
				return err
			}
		}
	}
	for _, cell := range ix.lists {
		if err := binary.Write(w, binary.LittleEndian, int32(len(cell))); err != nil {
			return err
		}
		for _, r := range cell {
			if err := binary.Write(w, binary.LittleEndian, int32(len(r.ID))); err != nil {
				return err
			}
			if _, err := w.Write([]byte(r.ID)); err != nil {
				return err
			}
			for _, v := range r.Vec {
				if err := binary.Write(w, binary.LittleEndian, v); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// loadSnapshot imports the index from path (empty index when absent).
func (ix *Index) loadSnapshot() error {
	f, err := os.Open(ix.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 1<<20)
	return ix.readSnapshot(br)
}

func (ix *Index) readSnapshot(r io.Reader) error {
	magic := make([]byte, len(snapshotMagic))
	if _, err := io.ReadFull(r, magic); err != nil {
		return fmt.Errorf("ann snapshot: %w", err)
	}
	if string(magic) != snapshotMagic {
		return fmt.Errorf("ann snapshot: bad magic %q", magic)
	}
	var hdr [3]int32
	if err := binary.Read(r, binary.LittleEndian, &hdr); err != nil {
		return err
	}
	dim, nlist := int(hdr[0]), int(hdr[1])
	if dim != ix.params.Dim {
		return fmt.Errorf("ann snapshot: dim %d != params dim %d", dim, ix.params.Dim)
	}
	if nlist <= 0 || nlist > 1<<20 {
		return fmt.Errorf("ann snapshot: bad nlist %d", nlist)
	}
	centroids := make([][]float32, nlist)
	for i := range centroids {
		c := make([]float32, dim)
		if err := binary.Read(r, binary.LittleEndian, c); err != nil {
			return err
		}
		centroids[i] = c
	}
	ix.centroids = centroids
	ix.lists = make([][]Row, nlist)
	ix.idCell = make(map[string]int, 1<<20)
	var cnt int32
	for c := 0; c < nlist; c++ {
		if err := binary.Read(r, binary.LittleEndian, &cnt); err != nil {
			return err
		}
		if cnt < 0 || cnt > 1<<26 {
			return fmt.Errorf("ann snapshot: bad cell count %d", cnt)
		}
		cell := make([]Row, 0, cnt)
		for j := 0; j < int(cnt); j++ {
			var idLen int32
			if err := binary.Read(r, binary.LittleEndian, &idLen); err != nil {
				return err
			}
			if idLen < 0 || idLen > 1<<16 {
				return fmt.Errorf("ann snapshot: bad id len %d", idLen)
			}
			id := make([]byte, idLen)
			if _, err := io.ReadFull(r, id); err != nil {
				return err
			}
			vec := make([]float32, dim)
			if err := binary.Read(r, binary.LittleEndian, vec); err != nil {
				return err
			}
			cell = append(cell, Row{ID: string(id), Vec: vec})
			ix.idCell[string(id)] = c
		}
		ix.lists[c] = cell
	}
	return nil
}

// replayWAL appends every WAL entry to the in-memory index. A torn tail
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
			if _, exists := ix.idCell[row.ID]; exists {
				continue
			}
			if len(ix.centroids) == 0 {
				ix.centroids = [][]float32{row.Vec}
				ix.lists = make([][]Row, 1)
			}
			c := nearestCell(row.Vec, ix.centroids)
			ix.lists[c] = append(ix.lists[c], row)
			ix.idCell[row.ID] = c
			ix.dirty++
		} else {
			ix.skipped++
		}
	}
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
		all = append(all, scored{r: r, cos: float32(cosine(q, r.Vec))})
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

// nearestCell returns the index of the centroid closest (cosine) to v.
func nearestCell(v []float32, centroids [][]float32) int {
	best, bestSim := 0, -2.0
	for i, c := range centroids {
		s := cosine(v, c)
		if s > bestSim {
			bestSim = s
			best = i
		}
	}
	return best
}

// kmeans runs Lloyd's algorithm on sample with k centroids and iters rounds,
// seeded for determinism. Centers are initialized from random sample rows.
func kmeans(rows []Row, k, iters int, seed int64) [][]float32 {
	if k > len(rows) {
		k = len(rows)
	}
	rng := rand.New(rand.NewSource(seed))
	dim := len(rows[0].Vec)
	cent := make([][]float32, k)
	for i := range cent {
		c := rows[rng.Intn(len(rows))].Vec
		cent[i] = make([]float32, dim)
		copy(cent[i], c)
	}
	assign := make([]int, len(rows))
	workers := 8
	if len(rows) < workers {
		workers = len(rows)
	}
	if workers < 1 {
		workers = 1
	}
	for it := 0; it < iters; it++ {
		// assignment pass (parallel)
		{
			var wg sync.WaitGroup
			jobs := make(chan int)
			wg.Add(workers)
			for w := 0; w < workers; w++ {
				go func() {
					defer wg.Done()
					for i := range jobs {
						assign[i] = nearestCell(rows[i].Vec, cent)
					}
				}()
			}
			for i := range rows {
				jobs <- i
			}
			close(jobs)
			wg.Wait()
		}
		// mean recompute
		sum := make([][]float64, k)
		cnt := make([]int, k)
		for i := range sum {
			sum[i] = make([]float64, dim)
		}
		for i, r := range rows {
			c := assign[i]
			cnt[c]++
			for j, v := range r.Vec {
				sum[c][j] += float64(v)
			}
		}
		for c := 0; c < k; c++ {
			if cnt[c] == 0 {
				continue
			}
			for j := 0; j < dim; j++ {
				cent[c][j] = float32(sum[c][j] / float64(cnt[c]))
			}
		}
	}
	return cent
}

// cosine computes cosine similarity (float64 accumulation; float32 inputs
// match the stored embeddings exactly, and the baseline scan uses the same
// values — so candidate order is identical when the candidate set matches).
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// cosine64 is the exact scan's cosine: float64 query vs float32 stored
// values, accumulated in float64 (mirror of brain.cosineToQuery).
func cosine64(q []float64, v []float32) float64 {
	n := len(q)
	if len(v) < n {
		n = len(v)
	}
	var dot, nq, nv float64
	for i := 0; i < n; i++ {
		dot += q[i] * float64(v[i])
		nq += q[i] * q[i]
		nv += float64(v[i]) * float64(v[i])
	}
	if nq == 0 || nv == 0 {
		return 0
	}
	return dot / (math.Sqrt(nq) * math.Sqrt(nv))
}

func zeroNorm64(q []float64) bool {
	for _, f := range q {
		if f != 0 {
			return false
		}
	}
	return true
}

func float64Vec(v []float32) []float64 {
	out := make([]float64, len(v))
	for i, f := range v {
		out[i] = float64(f)
	}
	return out
}
