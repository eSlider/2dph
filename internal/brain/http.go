//go:build cgo && system_ladybug

package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// Ready opens the Ladybug file for the life of the serve process.
func Ready() error {
	return openBrain()
}

// HTTP is the in-process API used by bin/brain/serve.go.
type HTTP struct{}

func (HTTP) Search(_ context.Context, query string, limit int) ([]byte, error) {
	hits, err := searchHits(query, "", "", limit)
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
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(toJSONOut(hits, query, "")); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (HTTP) Get(_ context.Context, id string, body bool) ([]byte, error) {
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
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, count(*)")
	if err != nil {
		return nil, err
	}
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
	return json.Marshal(map[string]any{"total": total, "by_root": byRoot, "db": dbPath()})
}

func (HTTP) Audit(context.Context) ([]byte, error) {
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, l.confidence, count(*)")
	if err != nil {
		return nil, err
	}
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

func (HTTP) Ingest(context.Context) ([]byte, error) {
	return json.Marshal(map[string]any{
		"mode":    "rebuild",
		"command": "bin/brain/index.go --rebuild",
		"add":     "v2",
	})
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
