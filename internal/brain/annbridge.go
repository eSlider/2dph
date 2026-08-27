//go:build cgo && system_ladybug

package brain

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/eSlider/2dph/internal/brain/ann"
	"github.com/eSlider/2dph/internal/config"
)

// ANN integration (issue #204): an incremental HNSW index outside liblbug
// (whose own HNSW SIGSEGVs on grown graphs, #192). The index lives at
// var/state/vector.ann (+ .wal), is built/upserted by bin/brain/ann.go and
// read here for the query vector path. Missing/corrupt/disabled index falls
// back to the brute-force cosine scan — search never fails because of ANN.

// annMode controls the vector path for a process.
type annMode int

const (
	// annAuto follows config vector.ann.enabled (serve/CLI default).
	annAuto annMode = iota
	// annForceOn forces ANN (bench --candidate inproc-ann); falls back to
	// the scan only when the index is unavailable.
	annForceOn
	// annForceOff forces the linear scan (bench baseline, regardless of
	// config), isolating the A/B to the vector layer.
	annForceOff
)

var (
	annMu     sync.RWMutex
	curAnnMod = annAuto
	annIdx    *ann.Index // lazily opened, cached for the process lifetime
	annOpen   error      // sticky: corrupt index → fallback until restart
	annStats  = annInfo{}
)

type annInfo struct {
	Loaded  bool   `json:"loaded"` // true when a non-empty index is serving (Len > 0)
	Len     int    `json:"len"`
	Path    string `json:"path"`
	Params  string `json:"params,omitempty"`
	Skipped int    `json:"skipped,omitempty"`
	Err     string `json:"error,omitempty"`
}

func setAnnMode(m annMode) {
	annMu.Lock()
	defer annMu.Unlock()
	curAnnMod = m
}

// annEnabled reports whether the vector path should try ANN.
func annEnabled() bool {
	annMu.RLock()
	defer annMu.RUnlock()
	switch curAnnMod {
	case annForceOff:
		return false
	case annForceOn:
		return true
	}
	return brainCfg().Vector.ANN.Enabled
}

// annIndex returns the lazily opened index. A missing index yields an empty
// index (not an error) so callers fall back; a corrupt snapshot is sticky
// error — also a fallback.
func annIndex() (*ann.Index, error) {
	annMu.RLock()
	if annIdx != nil || annOpen != nil || !annEnabled() {
		idx, err := annIdx, annOpen
		annMu.RUnlock()
		return idx, err
	}
	annMu.RUnlock()

	annMu.Lock()
	defer annMu.Unlock()
	if annIdx != nil || annOpen != nil {
		return annIdx, annOpen
	}
	cfg := brainCfg().Vector.ANN
	path := cfg.Index
	if path == "" {
		path = filepath.Join(repoRoot(), "var", "state", "vector.ann")
	}
	p := annParams()
	idx, err := ann.Open(path, p)
	annIdx, annOpen = idx, err
	annStats.Path = path
	if err != nil {
		annStats.Err = err.Error()
		log.Printf("brain/ann: open %s: %v (fallback: linear scan)", path, err)
		return nil, err
	}
	annStats.Loaded = idx.Len() > 0
	annStats.Len = idx.Len()
	annStats.Skipped = idx.Skipped()
	annStats.Params = fmt.Sprintf("dim=%d nlist=%d nprobe=%d", p.Dim, p.NList, p.NProbe)
	log.Printf("brain/ann: index %s loaded (%d vectors, %s)", path, idx.Len(), annStats.Params)
	return idx, nil
}

// WarmANN eagerly opens the ANN index at serve startup (issue #206): the
// ~320MB snapshot loads once instead of on the first query, so the first
// search is already fast. Missing/empty/corrupt/disabled are non-fatal — the
// search path falls back to the linear scan, and the wave's ann-build step
// builds the index (serve never rebuilds: single-writer rule, #204).
func WarmANN() {
	idx, err := annIndex()
	if err != nil {
		log.Printf("brain/ann: warm start: %v (search falls back to the linear scan)", err)
		return
	}
	if idx != nil && idx.Len() == 0 {
		log.Printf("brain/ann: warm start: index empty — search falls back to the linear scan until the wave's ann-build step builds it")
	}
}

// annQueryVector ranks the top-k vector hits through the ANN index and loads
// their metadata from the graph. Returns (nil, nil) when the index is
// unavailable or the query vector is degenerate — the caller falls back.
func annQueryVector(emb []float64, limit int) ([]Hit, error) {
	idx, err := annIndex()
	if err != nil || idx == nil || idx.Len() == 0 {
		return nil, nil
	}
	res := idx.Search64(emb, limit)
	if len(res) == 0 {
		return nil, nil
	}
	out := make([]Hit, 0, len(res))
	for _, r := range res {
		h, err := leafHitByID(r.ID, float64(r.Score))
		if err != nil {
			// Leaf deleted between build and search: skip, not fatal.
			continue
		}
		out = append(out, h)
	}
	return out, nil
}

// leafHitByID loads one leaf's search fields by id (point lookup on the PK).
func leafHitByID(id string, score float64) (Hit, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return Hit{}, fmt.Errorf("brain not open")
	}
	stmt, err := conn.Prepare(
		"MATCH (l:Leaf {id:$id}) RETURN l.id, l.text, l.root, l.source, l.confidence, l.valid_from, l.valid_to",
	)
	if err != nil {
		return Hit{}, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"id": id})
	if err != nil {
		return Hit{}, err
	}
	defer res.Close()
	if !res.HasNext() {
		return Hit{}, fmt.Errorf("no leaf %s", id)
	}
	row, err := res.Next()
	if err != nil {
		return Hit{}, err
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) < 7 {
		return Hit{}, fmt.Errorf("leaf row")
	}
	return Hit{
		ID: fmt.Sprint(vals[0]), Text: fmt.Sprint(vals[1]),
		Root: fmt.Sprint(vals[2]), Source: fmt.Sprint(vals[3]),
		Confidence: fmt.Sprint(vals[4]), ValidFrom: nullStr(vals[5]),
		ValidTo: nullStr(vals[6]), Score: score,
	}, nil
}

// annParams returns the active ANN params (config or ann defaults), with
// defaults applied so the effective values are visible to callers.
func annParams() ann.Params {
	cfg := brainCfg().Vector.ANN
	p := ann.Params{
		Dim: cfg.Dim, NList: cfg.NList, NProbe: cfg.NProbe,
	}
	return p.WithDefaults()
}

// toFloat32 converts the embed daemon's []float64 to the index's float32
// dimension, padding/truncating to dim (the DB pads to 256 at write time).
func toFloat32(emb []float64, dim int) []float32 {
	out := make([]float32, dim)
	for i := 0; i < dim && i < len(emb); i++ {
		out[i] = float32(emb[i])
	}
	return out
}

// annStatsJSON is the /stats-style ANN report for the tools.
func annStatsJSON() annInfo {
	annMu.RLock()
	defer annMu.RUnlock()
	if annIdx != nil {
		annStats.Len = annIdx.Len()
		annStats.Skipped = annIdx.Skipped()
	}
	return annStats
}

// annOpenForBuild opens the index for the build/upsert tools, honoring the
// same config defaults (the tool passes an explicit path via --index).
func annOpenForBuild(path string) (*ann.Index, error) {
	p := annParams()
	if path == "" {
		path = filepath.Join(repoRoot(), "var", "state", "vector.ann")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return ann.Open(path, p)
}

// MainANN is the bin/brain/ann.go entry: subcommand dispatch for the ANN
// index tool (build/upsert/stats/api) plus the search contract of
// bin/brain/search.go (--json -n N --no-web "q") with the ANN vector path
// forced on — the --candidate binary for the A/B bench (#204). "serve" stays
// the /embed daemon spawn contract (ensureDaemon), "api" is the HTTP search
// server for bench --candidate http://…
func MainANN(args []string) int {
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/ann: config: %v\n", err)
		return 1
	}
	Configure(cfg)
	if len(args) > 0 {
		switch args[0] {
		case "build":
			return runAnnBuild(args[1:])
		case "upsert":
			return runAnnUpsert(args[1:])
		case "ensure":
			return runAnnEnsure(args[1:])
		case "stats":
			return runAnnStats(args[1:])
		case "api":
			return runAnnAPI(args[1:])
		}
	}
	setAnnMode(annForceOn)
	return runSearch(args)
}
