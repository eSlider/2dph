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
// stats, and the HTTP search server used as bench --candidate.

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
