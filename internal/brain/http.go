//go:build cgo && system_ladybug

package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/eSlider/2dph/internal/brain/rank"
)

// ingestMu serializes /ingest writes: the serve process holds kb.lbug open
// for its lifetime, and Ladybug dies silently if the same file is opened a
// second time inside one process. The read handle is released for the write
// window and reopened afterwards (see ingestWriteLocked).
var ingestMu sync.Mutex

// Ready opens the Ladybug file for the life of the serve process.
func Ready() error {
	return openBrain()
}

// HTTP is the in-process API used by bin/brain/serve.go.
type HTTP struct{}

func (HTTP) Search(ctx context.Context, query string, limit int, asOf, root, sort string, noWeb bool) ([]byte, error) {
	sortDate, sortDesc := false, false
	if sort != "" {
		if sort == "date" || sort == "date:asc" {
			sortDate = true
		} else if sort == "date:desc" {
			sortDate, sortDesc = true, true
		}
	}
	hits, err := searchHits(query, root, "", limit, asOf, sortDate, sortDesc)
	if err != nil {
		return nil, err
	}
	for i := range hits {
		if hits[i].Text != "" {
			runes := []rune(hits[i].Text)
			if len(runes) > 280 {
				runes = runes[:280]
			}
			hits[i].Snippet = string(runes)
		}
	}
	webOut := rank.Deduce(hits, query, root, noWeb, func(q string) rank.SecondSource {
		return lookupWeb(ctx, q)
	})
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(toJSONOut(hits, query, root, asOf, webOut)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (HTTP) Get(_ context.Context, id string, body bool) ([]byte, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	stmt, err := conn.Prepare(
		"MATCH (l:Leaf {id:$id}) RETURN l.id, l.text, l.root, l.confidence, l.source, l.type",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	defer res.Close()
	if !res.HasNext() {
		return nil, fmt.Errorf("no leaf %s", id)
	}
	row, err := res.Next()
	if err != nil {
		return nil, err
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) < 6 {
		return nil, fmt.Errorf("leaf row")
	}
	out := map[string]any{
		"id":         fmt.Sprint(vals[0]),
		"root":       fmt.Sprint(vals[2]),
		"confidence": fmt.Sprint(vals[3]),
		"source":     fmt.Sprint(vals[4]),
		"type":       fmt.Sprint(vals[5]),
	}
	if body {
		out["text"] = fmt.Sprint(vals[1])
	}
	return json.Marshal(out)
}

func (HTTP) Stats(context.Context) ([]byte, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, count(*)")
	if err != nil {
		return nil, err
	}
	defer res.Close()
	byRoot := map[string]int{}
	total := 0
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			continue
		}
		n := int(asInt(vals[1]))
		byRoot[fmt.Sprint(vals[0])] = n
		total += n
	}
	return json.Marshal(map[string]any{"total": total, "by_root": byRoot, "db": dbPath(), "ann": annStatsJSON()})
}

func (HTTP) Audit(context.Context) ([]byte, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, l.confidence, count(*)")
	if err != nil {
		return nil, err
	}
	defer res.Close()
	var rows []map[string]any
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 3 {
			continue
		}
		rows = append(rows, map[string]any{
			"root":       fmt.Sprint(vals[0]),
			"confidence": fmt.Sprint(vals[1]),
			"count":      asInt(vals[2]),
		})
	}
	return json.Marshal(map[string]any{"status": "ok", "by_confidence": rows})
}

// embedIngestLeafs fills missing embeddings for /ingest leafs. Package-level
// var so tests can run the full Ingest path offline, without the HF model.
var embedIngestLeafs = embedIngestLeafsWithModel

func embedIngestLeafsWithModel(leafs []LeafInput) error {
	model, err := getIngestModel()
	if err != nil {
		return fmt.Errorf("model: %w", err)
	}
	// Shared process-living model: never Close it here (Close frees only the
	// tokenizer and would break concurrent requests while leaking nothing).
	return embedLeafs(model, leafs)
}

func (HTTP) Ingest(ctx context.Context, body []byte) ([]byte, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return json.Marshal(map[string]any{
			"mode":    "add",
			"command": "bin/brain/add.go",
			"rebuild": "bin/brain/index.go --rebuild",
		})
	}
	leafs, err := parseIngestLeafs(body)
	if err != nil {
		return nil, err
	}
	if err := embedIngestLeafs(leafs); err != nil {
		return nil, err
	}
	ingestMu.Lock()
	defer ingestMu.Unlock()
	// Writers must own the swap: block all readers for the close → write →
	// reopen window so a handler never runs a statement against a connection
	// that Ingest just closed.
	brainMu.Lock()
	defer brainMu.Unlock()
	dbpath := dbPath()
	ids, err := ingestWriteLocked(dbpath, leafs)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"mode": "add", "ids": ids, "db": dbpath})
}

// ingestWriteLocked closes the serve read handle BEFORE opening kb.lbug
// writable — Ladybug dies silently in-process when the same file is opened
// twice — writes leafs through the private writable handle, then reopens the
// serve read connection so /search, /get and /stats see the new leafs without
// a restart. The reopen runs on every exit path — including
// InitSchema/AddLeafs/EnsureIndexes errors — so the process never lands in a
// "brain not open" state for its lifetime (Ladybug WAL snapshots the read
// connection at its first query). Caller holds brainMu.Lock.
func ingestWriteLocked(dbpath string, leafs []LeafInput) ([]string, error) {
	closeBrainLocked() // release the read handle: same-file double-open kills lbug
	wdb, wconn, err := OpenWritable(dbpath)
	if err != nil {
		_ = refreshBrainLocked() // restore serving before reporting the error
		return nil, err
	}
	closed := false
	closeW := func() {
		if !closed {
			closed = true
			if wconn != nil {
				wconn.Close()
			}
			if wdb != nil {
				wdb.Close()
			}
		}
	}
	restore := func() {
		closeW()
		_ = refreshBrainLocked()
	}
	defer restore()
	if err := InitSchema(wconn); err != nil {
		return nil, err
	}
	ids, err := AddLeafs(wconn, leafs)
	if err != nil {
		return nil, err
	}
	if err := EnsureIndexes(wconn); err != nil {
		return nil, err
	}
	// Ladybug only exposes the write to fresh readers once the writable
	// connection closes; the deferred restore() closes it and reopens the
	// serve read connection.
	return ids, nil
}

// refreshBrain reopens the long-lived serve read connection so data written
// by Ingest's own connection becomes visible (Ladybug WAL snapshots the read
// connection at its first query; without a reopen ingested facts stay hidden
// from /search, /get and /stats until the process restarts).
func refreshBrain() error {
	brainMu.Lock()
	defer brainMu.Unlock()
	return refreshBrainLocked()
}

// refreshBrainLocked assumes brainMu is held write-locked.
func refreshBrainLocked() error {
	closeBrainLocked()
	return openWithSandboxLocked(brainCfg().Eps)
}

func parseIngestLeafs(raw []byte) ([]LeafInput, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	var objs []map[string]any
	switch t := payload.(type) {
	case []any:
		for _, x := range t {
			m, ok := x.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("list items must be objects")
			}
			objs = append(objs, m)
		}
	case map[string]any:
		if leafs, ok := t["leafs"].([]any); ok {
			for _, x := range leafs {
				m, ok := x.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("leafs items must be objects")
				}
				objs = append(objs, m)
			}
		} else {
			objs = []map[string]any{t}
		}
	default:
		return nil, fmt.Errorf("json must be object, list, or {leafs:[...]}")
	}
	out := make([]LeafInput, 0, len(objs))
	for _, m := range objs {
		lf := LeafInput{
			Text: fmt.Sprint(m["text"]), Source: fmt.Sprint(m["source"]),
			Root: fmt.Sprint(m["root"]), Confidence: fmt.Sprint(m["confidence"]),
			SourceRev: fmt.Sprint(m["source_rev"]), How: fmt.Sprint(m["how"]),
			Loc: fmt.Sprint(m["loc"]), Type: fmt.Sprint(m["type"]),
			ValidFrom: fmt.Sprint(m["valid_from"]), ValidTo: fmt.Sprint(m["valid_to"]),
		}
		if lf.Text == "<nil>" {
			lf.Text = ""
		}
		if lf.Source == "<nil>" {
			lf.Source = ""
		}
		if lf.Text == "" || lf.Source == "" {
			return nil, fmt.Errorf("each leaf needs text and source")
		}
		out = append(out, lf)
	}
	return out, nil
}

func asInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}
