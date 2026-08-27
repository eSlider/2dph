//go:build cgo && system_ladybug

package brain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/eSlider/2dph/internal/brain/ann"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/httpapi"
)

// ANN index tooling (issue #204): build a fresh snapshot from the whole DB,
// upsert only new leafs (incremental, append-only WAL — never a rebuild),
// ensure (the wave's ann-build step: build when missing/stale, upsert
// otherwise), stats, and the HTTP search server used as bench --candidate.
// Issue #206 rolls the index into production: enabled by default, the wave
// maintains it incrementally, serve loads it at startup (warm start).

type annFlags struct {
	index, db string
	limit     int
	jsonOut   bool
	port      int
}

func parseAnnFlags(sub string, args []string) (annFlags, error) {
	var f annFlags
	p := cli.New("brain-ann-" + sub)
	p.String(&f.index, "", "index", "vector.ann path (default <root>/var/state/vector.ann)")
	p.String(&f.db, "", "db", "kb.lbug path (default <root>/var/kb.lbug)")
	p.Int(&f.limit, "", "limit", "max vectors to index (build debug)")
	p.Bool(&f.jsonOut, "", "json", "JSON output")
	p.Int(&f.port, "", "port", "api port (default 8631)")
	if err := cli.Parse(p, args); err != nil {
		return f, err
	}
	return f, nil
}

// openAnnToolDB opens the DB read-only-ish for extraction, honoring --db.
func openAnnToolDB(dbPath string) error {
	if dbPath != "" {
		prev := dbPathFn
		dbPathFn = func() string { return dbPath }
		defer func() { dbPathFn = prev }()
	}
	if !brainOpen() {
		if err := openBrain(); err != nil {
			return fmt.Errorf("open brain: %w", err)
		}
	}
	return nil
}

// extractRows pulls every leaf (id, embedding) from the DB. Zero-norm and
// short embeddings are dropped by the ann package at build/upsert.
func extractRows(limit int) ([]ann.Row, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	stmt, err := conn.Prepare("MATCH (l:Leaf) WHERE l.embedding IS NOT NULL RETURN l.id, l.embedding")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, nil)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	rows := make([]ann.Row, 0, 320000)
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			continue
		}
		e, ok := vals[1].([]any)
		if !ok || len(e) == 0 {
			continue
		}
		rows = append(rows, ann.Row{ID: fmt.Sprint(vals[0]), Vec: anyToFloat32(e)})
		if limit > 0 && len(rows) >= limit {
			break
		}
	}
	return rows, nil
}

func anyToFloat32(e []any) []float32 {
	out := make([]float32, len(e))
	for i, v := range e {
		switch f := v.(type) {
		case float32:
			out[i] = f
		case float64:
			out[i] = float32(f)
		}
	}
	return out
}

func runAnnBuild(args []string) int {
	f, err := parseAnnFlags("build", args)
	if err != nil {
		return cli.Fail(err)
	}
	if err := openAnnToolDB(f.db); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann build: %v\n", err)
		return 1
	}
	t0 := time.Now()
	rows, err := extractRows(f.limit)
	// Release the DB read handle before the (long) graph build so other
	// processes can open kb.lbug while we index (single-writer rule).
	closeBrain()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann build: extract: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "brain/ann build: extracted %d vectors in %s\n", len(rows), time.Since(t0).Round(time.Millisecond))
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "brain/ann build: nothing to index")
		return 0
	}
	path := f.index
	if path == "" {
		path = filepath.Join(repoRoot(), "var", "state", "vector.ann")
	}
	// A build is a full replacement: drop any previous snapshot (possibly
	// an older format) and its WAL before opening a fresh index.
	for _, p := range []string{path, path + ".wal"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "brain/ann build: remove %s: %v\n", p, err)
			return 1
		}
	}
	idx, err := annOpenForBuild(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann build: %v\n", err)
		return 1
	}
	t1 := time.Now()
	if err := idx.Build(rows); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann build: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "brain/ann build: built %d vectors in %s (skipped %d zero/short)\n",
		idx.Len(), time.Since(t1).Round(time.Millisecond), idx.Skipped())
	if err := idx.SaveTo(path); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann build: save: %v\n", err)
		return 1
	}
	if err := idx.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann build: %v\n", err)
		return 1
	}
	if f.jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"built": idx.Len(), "skipped": idx.Skipped(), "index": path,
			"elapsed_s": time.Since(t0).Seconds(),
		})
	} else {
		fmt.Printf("brain/ann: built %d vectors -> %s\n", idx.Len(), path)
	}
	return 0
}

func runAnnUpsert(args []string) int {
	f, err := parseAnnFlags("upsert", args)
	if err != nil {
		return cli.Fail(err)
	}
	if !brainCfg().Vector.ANN.Enabled {
		fmt.Fprintln(os.Stderr, "brain/ann upsert: vector.ann.enabled=false, index disabled (skip)")
		return cli.ExitSkip
	}
	path := f.index
	if path == "" {
		path = filepath.Join(repoRoot(), "var", "state", "vector.ann")
	}
	idx, err := annOpenForBuild(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann upsert: %v\n", err)
		return 1
	}
	defer idx.Close()
	if err := openAnnToolDB(f.db); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann upsert: %v\n", err)
		return 1
	}
	t0 := time.Now()
	rows, err := extractRows(f.limit)
	closeBrain() // release kb.lbug before graph ops (single-writer rule)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann upsert: extract: %v\n", err)
		return 1
	}
	extractDur := time.Since(t0)
	before := idx.Len()
	// Filter to genuinely new ids before handing to the index.
	newRows := rows[:0]
	for _, r := range rows {
		if !idx.Lookup(r.ID) {
			newRows = append(newRows, r)
		}
	}
	t1 := time.Now()
	if err := idx.Upsert(newRows); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann upsert: %v\n", err)
		return 1
	}
	upsertDur := time.Since(t1)
	added := idx.Len() - before
	if f.jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"indexed": idx.Len(), "added": added, "skipped": idx.Skipped(),
			"dirty_wal": idx.Dirty(), "extract_s": extractDur.Seconds(),
			"upsert_s": upsertDur.Seconds(), "rebuild": false,
		})
	} else {
		fmt.Printf("brain/ann: upsert added %d (index %d, WAL %d) extract %s + upsert %s — no rebuild\n",
			added, idx.Len(), idx.Dirty(), extractDur.Round(time.Millisecond), upsertDur.Round(time.Millisecond))
	}
	return 0
}

// walCompactThreshold: WAL lines accumulated since the last snapshot before
// the incremental path also rewrites the snapshot and truncates the WAL. The
// WAL grows one line per upserted vector; without compaction the serve warm
// start would replay an unbounded WAL. A compaction is a Save (snapshot
// rewrite), never a rebuild — no k-means, cells stay as they are. Var so
// tests can lower it without building a 90k-row fixture.
var walCompactThreshold = 10000

// annEnsurePlan decides what the wave's ann-build step must do: a full build
// when there is no index or it is stale (missing >10% of the DB vectors —
// e.g. built from a partial export or before a bulk import), otherwise the
// incremental WAL-upsert. Steady-state growth (a few hundred new leafs on
// 313k) stays incremental — the wave never rebuilds per wave.
func annEnsurePlan(idxLen, rowsLen int) (build bool, reason string) {
	if rowsLen <= 0 {
		return false, "nothing to index"
	}
	if idxLen <= 0 {
		return true, "no index"
	}
	if idxLen*10 < rowsLen*9 {
		return true, fmt.Sprintf("index stale (%d vectors vs %d in DB)", idxLen, rowsLen)
	}
	return false, "incremental"
}

// runAnnEnsure is the wave's ann-build step (issue #206): maintain the ANN
// index so it covers the DB — full build only when the index is missing or
// stale, otherwise a WAL-upsert of new leafs (<1s per wave, never a rebuild).
// SKIPs (exit 3) when vector.ann.enabled=false.
func runAnnEnsure(args []string) int {
	f, err := parseAnnFlags("ensure", args)
	if err != nil {
		return cli.Fail(err)
	}
	if !brainCfg().Vector.ANN.Enabled {
		fmt.Fprintln(os.Stderr, "brain/ann ensure: vector.ann.enabled=false, index disabled (skip)")
		return cli.ExitSkip
	}
	path := f.index
	if path == "" {
		path = filepath.Join(repoRoot(), "var", "state", "vector.ann")
	}
	idx, err := annOpenForBuild(path)
	if err != nil {
		// Corrupt snapshot: drop it and rebuild from scratch — the wave must
		// never fail on a damaged index.
		fmt.Fprintf(os.Stderr, "brain/ann ensure: open %s: %v (full rebuild)\n", path, err)
		for _, p := range []string{path, path + ".wal"} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "brain/ann ensure: remove %s: %v\n", p, err)
				return 1
			}
		}
		idx = ann.New(annParams())
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) && idx.Len() > 0 {
		// WAL-only index (snapshot removed while the WAL survived): no durable
		// base, replay grows forever — rebuild to restore the snapshot.
		fmt.Fprintf(os.Stderr, "brain/ann ensure: snapshot %s missing, %d vectors WAL-only — full rebuild\n", path, idx.Len())
		if err := os.Remove(path + ".wal"); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "brain/ann ensure: remove %s: %v\n", path+".wal", err)
			return 1
		}
		idx = ann.New(annParams())
	}
	defer idx.Close()

	if err := openAnnToolDB(f.db); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann ensure: %v\n", err)
		return 1
	}
	t0 := time.Now()
	rows, err := extractRows(f.limit)
	closeBrain() // release kb.lbug before graph ops (single-writer rule)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann ensure: extract: %v\n", err)
		return 1
	}
	extractDur := time.Since(t0)
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "brain/ann ensure: no leafs with embeddings to index")
		return 0
	}

	build, reason := annEnsurePlan(idx.Len(), len(rows))
	if build {
		t1 := time.Now()
		if err := idx.Build(rows); err != nil {
			fmt.Fprintf(os.Stderr, "brain/ann ensure: build: %v\n", err)
			return 1
		}
		if err := idx.SaveTo(path); err != nil {
			fmt.Fprintf(os.Stderr, "brain/ann ensure: save: %v\n", err)
			return 1
		}
		buildDur := time.Since(t1)
		if f.jsonOut {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"action": "build", "reason": reason, "indexed": idx.Len(),
				"skipped": idx.Skipped(), "rebuild": true, "index": path,
				"extract_s": extractDur.Seconds(), "build_s": buildDur.Seconds(),
			})
		} else {
			fmt.Printf("brain/ann: ensure built %d vectors -> %s (%s) extract %s + build %s\n",
				idx.Len(), path, reason, extractDur.Round(time.Millisecond), buildDur.Round(time.Millisecond))
		}
		return 0
	}

	// Incremental path: WAL-upsert only the genuinely new ids.
	before := idx.Len()
	newRows := rows[:0]
	for _, r := range rows {
		if !idx.Lookup(r.ID) {
			newRows = append(newRows, r)
		}
	}
	if len(newRows) == 0 {
		if f.jsonOut {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"action": "uptodate", "reason": reason, "indexed": idx.Len(),
				"added": 0, "rebuild": false, "index": path,
			})
		} else {
			fmt.Printf("brain/ann: ensure index up to date (%d vectors)\n", idx.Len())
		}
		return 0
	}
	t1 := time.Now()
	if err := idx.Upsert(newRows); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann ensure: upsert: %v\n", err)
		return 1
	}
	upsertDur := time.Since(t1)
	added := idx.Len() - before
	if added == 0 {
		// Only degenerate rows (zero-norm / short embeddings — unindexable by
		// design, the scan skips them too) were "new": the WAL is untouched,
		// the index covers everything addable — report up-to-date.
		if f.jsonOut {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
				"action": "uptodate", "reason": reason, "indexed": idx.Len(),
				"added": 0, "skipped": idx.Skipped(), "rebuild": false, "index": path,
			})
		} else {
			fmt.Printf("brain/ann: ensure index up to date (%d vectors, %d degenerate skipped)\n",
				idx.Len(), idx.Skipped())
		}
		return 0
	}
	compacted := false
	var saveDur time.Duration
	if idx.Dirty() > walCompactThreshold {
		// WAL got large: rewrite the snapshot and truncate the WAL so the
		// serve warm start never replays an unbounded WAL. Not a rebuild.
		t2 := time.Now()
		if err := idx.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "brain/ann ensure: compact save: %v\n", err)
			return 1
		}
		saveDur = time.Since(t2)
		compacted = true
	}
	if f.jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"action": "upsert", "reason": reason, "indexed": idx.Len(),
			"added": added, "skipped": idx.Skipped(), "dirty_wal": idx.Dirty(),
			"rebuild": false, "compacted": compacted, "index": path,
			"extract_s": extractDur.Seconds(), "upsert_s": upsertDur.Seconds(),
			"save_s": saveDur.Seconds(),
		})
	} else {
		comp := ""
		if compacted {
			comp = fmt.Sprintf(" + compact %s", saveDur.Round(time.Millisecond))
		}
		fmt.Printf("brain/ann: ensure upsert added %d (index %d, WAL %d) extract %s + upsert %s%s — no rebuild\n",
			added, idx.Len(), idx.Dirty(), extractDur.Round(time.Millisecond), upsertDur.Round(time.Millisecond), comp)
	}
	return 0
}

func runAnnStats(args []string) int {
	f, err := parseAnnFlags("stats", args)
	if err != nil {
		return cli.Fail(err)
	}
	path := f.index
	if path == "" {
		path = filepath.Join(repoRoot(), "var", "state", "vector.ann")
	}
	idx, err := annOpenForBuild(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann stats: %v\n", err)
		return 1
	}
	defer idx.Close()
	if f.jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"index": path, "len": idx.Len(), "dirty_wal": idx.Dirty(),
			"skipped": idx.Skipped(),
		})
	} else {
		fmt.Printf("index %s: %d vectors, WAL %d, skipped %d\n", path, idx.Len(), idx.Dirty(), idx.Skipped())
	}
	return 0
}

func runAnnAPI(args []string) int {
	f, err := parseAnnFlags("api", args)
	if err != nil {
		return cli.Fail(err)
	}
	setAnnMode(annForceOn)
	if err := Ready(); err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann api: %v\n", err)
		return 1
	}
	cfg := brainCfg()
	cfg.Host = "127.0.0.1"
	if f.port > 0 {
		cfg.Port = f.port
	}
	httpapi.Run(HTTP{}, &cfg)
	return 0
}
