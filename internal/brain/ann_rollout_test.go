//go:build cgo && system_ladybug

// ANN rollout tests (issue #206): the wave's ann-build ensure step (build
// when missing/stale, incremental WAL-upsert otherwise — never a rebuild per
// wave), the serve warm start (index loaded at startup, fallback on
// missing/corrupt), and the config enabled/disabled wiring. All offline on
// synthetic fixtures.
package brain

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/brain/ann"
	"github.com/eSlider/2dph/pkg/cli"
)

// captureToolStdout runs fn while stdout is redirected to a pipe and returns
// what fn wrote to stdout (JSON tool reports land there).
func captureToolStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b)
}

// annEnsureReport is the JSON contract of `bin/brain/ann.go ensure --json`.
type annEnsureReport struct {
	Action    string  `json:"action"`
	Reason    string  `json:"reason,omitempty"`
	Indexed   int     `json:"indexed"`
	Added     int     `json:"added"`
	Skipped   int     `json:"skipped,omitempty"`
	DirtyWAL  int     `json:"dirty_wal,omitempty"`
	Rebuild   bool    `json:"rebuild"`
	Compacted bool    `json:"compacted,omitempty"`
	ExtractS  float64 `json:"extract_s,omitempty"`
	BuildS    float64 `json:"build_s,omitempty"`
	UpsertS   float64 `json:"upsert_s,omitempty"`
	SaveS     float64 `json:"save_s,omitempty"`
	Index     string  `json:"index"`
}

func parseEnsureReport(t *testing.T, out string) annEnsureReport {
	t.Helper()
	var r annEnsureReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("parse ensure JSON %q: %v", out, err)
	}
	return r
}

// setAnnCfg points the process ANN config at index for the test and restores
// the previous config on cleanup.
func setAnnCfg(t *testing.T, enabled bool, index string) {
	t.Helper()
	prev := activeCfg
	activeCfg.Vector.ANN.Enabled = enabled
	activeCfg.Vector.ANN.Index = index
	t.Cleanup(func() { activeCfg = prev })
}

// buildFixtureIndex builds a persisted ANN snapshot over the fixture DB rows
// (mirror of what bin/brain/ann.go build does, minus the DB extraction).
func buildFixtureIndex(t *testing.T, dbpath, idxPath string) {
	t.Helper()
	rows, err := extractRows(0)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestAnnEnsurePlan pins the build/upsert decision: a missing index and a
// stale index (missing >10% of the DB vectors) trigger a full build;
// steady-state growth (a few hundred new leafs on 313k) stays incremental.
func TestAnnEnsurePlan(t *testing.T) {
	cases := []struct {
		name          string
		idxLen, rowsN int
		wantBuild     bool
		wantReasonSub string
	}{
		{"no index", 0, 100, true, "no index"},
		{"empty db", 0, 0, false, "nothing"},
		{"fresh", 313000, 313000, false, "incremental"},
		{"wave growth +100", 313000, 313100, false, "incremental"},
		{"wave growth +1000", 313000, 314000, false, "incremental"},
		{"stale -10%", 280000, 313000, true, "stale"},
		{"stale -90%", 10000, 313000, true, "stale"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			build, reason := annEnsurePlan(c.idxLen, c.rowsN)
			if build != c.wantBuild {
				t.Fatalf("annEnsurePlan(%d,%d) build = %v, want %v (reason %q)", c.idxLen, c.rowsN, build, c.wantBuild, reason)
			}
			if !strings.Contains(reason, c.wantReasonSub) {
				t.Errorf("reason %q does not mention %q", reason, c.wantReasonSub)
			}
		})
	}
}

// TestAnnEnsureBuildsWhenMissing: the first wave run on a corpus without an
// index must do a full build and land a searchable snapshot.
func TestAnnEnsureBuildsWhenMissing(t *testing.T) {
	const n, nClusters = 300, 10
	dbpath := annFixtureDB(t, n, nClusters)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, true, idxPath)

	out := captureToolStdout(t, func() {
		// runAnnEnsure returns an exit code; callers (wave) classify 3 = SKIP.
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("runAnnEnsure = %d, want 0", code)
		}
	})
	rep := parseEnsureReport(t, out)
	if rep.Action != "build" || !rep.Rebuild {
		t.Fatalf("first ensure = %+v, want action=build rebuild=true", rep)
	}
	if rep.Indexed != n {
		t.Fatalf("indexed = %d, want %d", rep.Indexed, n)
	}
	if _, err := os.Stat(idxPath); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}
}

// TestAnnEnsureIncrementalUpsertNoRebuild is the incrementality acceptance:
// after a build, adding +100 leafs and re-running ensure must take the
// WAL-upsert path (rebuild=false, added=100, sub-second) — the wave never
// does a full rebuild for steady-state growth. The fixture mirrors the
// production proportions: +100 on a large corpus, far under the 10%
// staleness threshold.
func TestAnnEnsureIncrementalUpsertNoRebuild(t *testing.T) {
	const baseN, extraN, nClusters = 2000, 100, 10
	dbpath := annFixtureDB(t, baseN, nClusters)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, true, idxPath)

	captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("initial ensure = %d, want 0", code)
		}
	})

	// Add 100 new leafs through the normal write path (the mail-index step).
	extra := annFixtureLeafs(extraN, nClusters)
	for i := range extra {
		extra[i].Text = "ANN ensure new " + itoa(i) + ": incremental wave leaf"
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

	start := time.Now()
	out := captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("second ensure = %d, want 0", code)
		}
	})
	elapsed := time.Since(start)
	rep := parseEnsureReport(t, out)
	if rep.Action != "upsert" || rep.Rebuild {
		t.Fatalf("second ensure = %+v, want action=upsert rebuild=false", rep)
	}
	if rep.Added != extraN {
		t.Fatalf("added = %d, want %d", rep.Added, extraN)
	}
	if rep.Indexed != baseN+extraN {
		t.Fatalf("indexed = %d, want %d", rep.Indexed, baseN+extraN)
	}
	if rep.DirtyWAL != extraN {
		t.Fatalf("dirty_wal = %d, want %d (append-only WAL, no rebuild)", rep.DirtyWAL, extraN)
	}
	// The "WAL-upsert <1s на +100" gate measures the upsert phase (the
	// extract phase is O(corpus) and runs regardless of ANN). Under -race the
	// wall time is inflated, but the upsert itself stays sub-second.
	if rep.UpsertS >= 1.0 {
		t.Fatalf("upsert of %d vectors took %.2fs >= 1s (incremental must be fast)", extraN, rep.UpsertS)
	}
	t.Logf("ann-build incremental: +%d vectors in %s wall (upsert %.3fs), rebuild=false, WAL=%d",
		extraN, elapsed, rep.UpsertS, rep.DirtyWAL)
}

// TestAnnEnsureUpToDateIsNoop: an index that already covers the DB is a
// no-op (action=uptodate, no WAL growth) — idempotent waves.
func TestAnnEnsureUpToDateIsNoop(t *testing.T) {
	const n, nClusters = 200, 8
	dbpath := annFixtureDB(t, n, nClusters)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, true, idxPath)

	captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("initial ensure = %d, want 0", code)
		}
	})
	out := captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("second ensure = %d, want 0", code)
		}
	})
	rep := parseEnsureReport(t, out)
	if rep.Action != "uptodate" || rep.Rebuild {
		t.Fatalf("up-to-date ensure = %+v, want action=uptodate rebuild=false", rep)
	}
	if rep.Added != 0 {
		t.Fatalf("added = %d, want 0", rep.Added)
	}
}

// TestAnnEnsureCorruptIndexRebuilds: a corrupt snapshot must not break the
// wave — ensure removes it and lands a fresh full build.
func TestAnnEnsureCorruptIndexRebuilds(t *testing.T) {
	const n, nClusters = 200, 8
	dbpath := annFixtureDB(t, n, nClusters)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	if err := os.WriteFile(idxPath, []byte("garbage snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}
	setAnnCfg(t, true, idxPath)

	out := captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("runAnnEnsure on corrupt index = %d, want 0", code)
		}
	})
	rep := parseEnsureReport(t, out)
	if rep.Action != "build" || !rep.Rebuild {
		t.Fatalf("corrupt-index ensure = %+v, want action=build rebuild=true", rep)
	}
	if rep.Indexed != n {
		t.Fatalf("indexed = %d, want %d", rep.Indexed, n)
	}
}

// TestAnnEnsureCompactsWhenWalLarge: the WAL grows one line per upserted
// vector; once it crosses the compaction threshold the incremental path also
// rewrites the snapshot (Save) and truncates the WAL — still NOT a rebuild
// (no k-means), so the serve warm start never replays a huge WAL. The
// threshold is lowered so the fixture stays under the 10% staleness rule.
func TestAnnEnsureCompactsWhenWalLarge(t *testing.T) {
	prev := walCompactThreshold
	walCompactThreshold = 200
	t.Cleanup(func() { walCompactThreshold = prev })
	const baseN, extraN, nClusters = 3000, 250, 8
	dbpath := annFixtureDB(t, baseN, nClusters)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, true, idxPath)

	captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("initial ensure = %d, want 0", code)
		}
	})

	extra := annFixtureLeafs(extraN, nClusters)
	for i := range extra {
		extra[i].Text = "ANN ensure compact " + itoa(i) + ": bulk wave leaf"
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

	out := captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("bulk ensure = %d, want 0", code)
		}
	})
	rep := parseEnsureReport(t, out)
	if rep.Action != "upsert" || rep.Rebuild {
		t.Fatalf("bulk ensure = %+v, want action=upsert rebuild=false", rep)
	}
	if !rep.Compacted {
		t.Fatalf("bulk ensure = %+v, want compacted=true (WAL crossed the threshold)", rep)
	}
	if rep.DirtyWAL != 0 {
		t.Fatalf("dirty_wal = %d, want 0 after compaction", rep.DirtyWAL)
	}
	if rep.Indexed != baseN+extraN {
		t.Fatalf("indexed = %d, want %d", rep.Indexed, baseN+extraN)
	}

	// Reopen: the snapshot contains every vector, no WAL to replay.
	idx2, err := ann.Open(idxPath, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	defer idx2.Close()
	if idx2.Len() != baseN+extraN || idx2.Dirty() != 0 {
		t.Fatalf("reopened len/dirty = %d/%d, want %d/0", idx2.Len(), idx2.Dirty(), baseN+extraN)
	}
}

// TestAnnEnsureWalOnlyRebuilds: a WAL without its snapshot (the snapshot was
// removed while the WAL survived) has no durable base — ensure must rebuild
// instead of appending to an orphaned WAL.
func TestAnnEnsureWalOnlyRebuilds(t *testing.T) {
	const n, nClusters = 200, 8
	dbpath := annFixtureDB(t, n, nClusters)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, true, idxPath)

	captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("initial ensure = %d, want 0", code)
		}
	})
	// Upsert one wave (WAL non-empty), then delete the snapshot: replay-only.
	captureToolStdout(t, func() {
		extra := annFixtureLeafs(1, 1)
		extra[0].Text = "ANN ensure wal-only leaf"
		db, conn, err := OpenWritable(dbpath)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := AddLeafs(conn, extra); err != nil {
			t.Fatal(err)
		}
		db.Close()
		conn.Close()
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("wave ensure = %d, want 0", code)
		}
	})
	if err := os.Remove(idxPath); err != nil {
		t.Fatal(err)
	}

	out := captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("wal-only ensure = %d, want 0", code)
		}
	})
	rep := parseEnsureReport(t, out)
	if rep.Action != "build" || !rep.Rebuild {
		t.Fatalf("wal-only ensure = %+v, want action=build rebuild=true", rep)
	}
	if rep.Indexed != n+1 {
		t.Fatalf("indexed = %d, want %d", rep.Indexed, n+1)
	}
	if _, err := os.Stat(idxPath); err != nil {
		t.Fatalf("snapshot not restored: %v", err)
	}
}

// TestAnnEnsureDegenerateRowsReportUptodate: leafs whose embeddings are
// zero-norm are unindexable by design (the scan skips them too) — ensure must
// not count them as additions or grow the WAL, and must report up-to-date.
func TestAnnEnsureDegenerateRowsReportUptodate(t *testing.T) {
	const n, nClusters = 200, 8
	dbpath := annFixtureDB(t, n, nClusters)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, true, idxPath)

	captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("initial ensure = %d, want 0", code)
		}
	})

	// Add leafs with zero embeddings (degenerate, unindexable).
	degenerate := annFixtureLeafs(3, 1)
	for i := range degenerate {
		degenerate[i].Text = "ANN ensure degenerate " + itoa(i)
		degenerate[i].Embedding = make([]float64, EmbedDim)
	}
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddLeafs(conn, degenerate); err != nil {
		t.Fatal(err)
	}
	db.Close()
	conn.Close()

	out := captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("degenerate ensure = %d, want 0", code)
		}
	})
	rep := parseEnsureReport(t, out)
	if rep.Action != "uptodate" || rep.Rebuild {
		t.Fatalf("degenerate ensure = %+v, want action=uptodate rebuild=false", rep)
	}
	if rep.Added != 0 || rep.Indexed != n {
		t.Fatalf("degenerate ensure indexed/added = %d/%d, want %d/0", rep.Indexed, rep.Added, n)
	}
	if rep.Skipped != 3 {
		t.Fatalf("skipped = %d, want 3 (zero-norm rows refused)", rep.Skipped)
	}
}

// TestAnnEnsureDisabledSkips: with vector.ann.enabled=false the wave step is
// a deliberate SKIP (exit code 3), never a failure.
func TestAnnEnsureDisabledSkips(t *testing.T) {
	const n, nClusters = 100, 5
	dbpath := annFixtureDB(t, n, nClusters)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, false, idxPath)

	if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != cli.ExitSkip {
		t.Fatalf("disabled ensure = %d, want SKIP %d", code, cli.ExitSkip)
	}
}

// TestAnnWarmStartLoadsIndex: serve startup (WarmANN) loads the existing
// snapshot eagerly — stats report loaded with the vector count.
func TestAnnWarmStartLoadsIndex(t *testing.T) {
	const n, nClusters = 300, 10
	dbpath := annFixtureDB(t, n, nClusters)
	openFixtureRead(t, dbpath)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	buildFixtureIndex(t, dbpath, idxPath)
	setANNIndexPath(t, idxPath)
	setAnnCfg(t, true, idxPath)

	WarmANN()
	st := annStatsJSON()
	if !st.Loaded {
		t.Fatal("warm start must load the existing index")
	}
	if st.Len != n {
		t.Fatalf("warm start len = %d, want %d", st.Len, n)
	}
}

// TestAnnWarmStartMissingFallsBack: no index on disk → warm start leaves the
// search path on the linear-scan fallback; queryVector still returns hits.
func TestAnnWarmStartMissingFallsBack(t *testing.T) {
	dbpath := annFixtureDB(t, 150, 6)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	openFixtureRead(t, dbpath)
	setANNIndexPath(t, idxPath)
	setAnnCfg(t, true, idxPath)

	WarmANN()
	st := annStatsJSON()
	if st.Loaded {
		t.Fatal("warm start with no index must not report loaded")
	}
	if st.Len != 0 {
		t.Fatalf("warm start len = %d, want 0 (empty index)", st.Len)
	}
	leafs := annFixtureLeafs(150, 6)
	hits, err := queryVector(leafs[2].Embedding, 5)
	if err != nil {
		t.Fatalf("queryVector fallback: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("search must still work through the fallback scan without an index")
	}
}

// TestAnnWarmStartCorruptFallsBack: a corrupt snapshot is sticky — warm
// start reports the error and search falls back to the scan, never fails.
func TestAnnWarmStartCorruptFallsBack(t *testing.T) {
	dbpath := annFixtureDB(t, 150, 6)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	if err := os.WriteFile(idxPath, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	openFixtureRead(t, dbpath)
	setANNIndexPath(t, idxPath)
	setAnnCfg(t, true, idxPath)

	WarmANN()
	st := annStatsJSON()
	if st.Loaded {
		t.Fatal("warm start must not report loaded for a corrupt index")
	}
	if st.Err == "" {
		t.Fatal("corrupt warm start must record the error")
	}
	leafs := annFixtureLeafs(150, 6)
	hits, err := queryVector(leafs[2].Embedding, 5)
	if err != nil {
		t.Fatalf("queryVector: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("search must fall back to the scan on a corrupt index")
	}
}

// TestAnnConfigDisabledDoesNotOpen: enabled=false keeps the index closed even
// with a valid snapshot present (annStats.Loaded stays false).
func TestAnnConfigDisabledDoesNotOpen(t *testing.T) {
	const n, nClusters = 200, 8
	dbpath := annFixtureDB(t, n, nClusters)
	openFixtureRead(t, dbpath)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	buildFixtureIndex(t, dbpath, idxPath)
	setANNIndexPath(t, idxPath)
	setAnnCfg(t, false, idxPath)

	WarmANN()
	st := annStatsJSON()
	if st.Loaded {
		t.Fatal("disabled ANN must not open the index")
	}
	if _, err := annIndex(); err != nil {
		t.Fatalf("annIndex with disabled config: %v", err)
	}
}
