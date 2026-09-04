//go:build cgo && system_ladybug

package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	lbug "github.com/LadybugDB/go-ladybug"
)

// Synapse Matrix surface (issue #82): expose leafs + edges (synapses) as a
// service for mcp-agent. Leafs are neurons, SYNAPTIC edges are synapses.
//
// All read paths hold brainMu.RLock for the whole query; AddEdge follows the
// same close→write→reopen window as Ingest so a handler never runs a statement
// against a connection a concurrent write just closed.

// queryLeafs returns leafs filtered by root/type/source (exact) and, when text
// is non-empty, ranked by the FTS index over the leaf text.
func queryLeafs(root, typ, source, text string, limit int) ([]map[string]any, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	if text != "" {
		return queryLeafsByText(root, typ, source, text, limit)
	}
	where := []string{}
	args := map[string]any{"n": limit}
	if root != "" {
		where = append(where, "l.root=$root")
		args["root"] = root
	}
	if typ != "" {
		where = append(where, "l.type=$type")
		args["type"] = typ
	}
	if source != "" {
		where = append(where, "l.source=$source")
		args["source"] = source
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	q := "MATCH (l:Leaf)" + clause +
		" RETURN l.id, l.text, l.root, l.type, l.source, l.confidence LIMIT $n"
	stmt, err := conn.Prepare(q)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, args)
	if err != nil {
		return nil, err
	}
	defer res.Close()
	return rowsToLeafs(res), nil
}

// queryLeafsByText searches leaf text through the FTS index, then applies the
// exact root/type/source filters in Go and cuts to limit.
func queryLeafsByText(root, typ, source, text string, limit int) ([]map[string]any, error) {
	stmt, err := conn.Prepare(
		"CALL QUERY_FTS_INDEX('Leaf', 'id', $q) " +
			"RETURN node.id, node.text, node.root, node.type, node.source, node.confidence, score " +
			"ORDER BY score DESC LIMIT $n",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"q": text, "n": limit * 3})
	if err != nil {
		return nil, err
	}
	defer res.Close()
	all := rowsToLeafs(res)
	out := make([]map[string]any, 0, limit)
	for _, lf := range all {
		if root != "" && lf["root"] != root {
			continue
		}
		if typ != "" && lf["type"] != typ {
			continue
		}
		if source != "" && lf["source"] != source {
			continue
		}
		out = append(out, lf)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// queryEdges returns the adjacency list of id: every synapse into or out of the
// leaf, deduplicated, with the synapse label.
func queryEdges(id string) ([]map[string]any, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	var out []map[string]any
	seen := map[string]bool{}
	// outgoing synapses: a -[r:SYNAPTIC]-> b
	if err := collectEdges(id, false, &out, seen); err != nil {
		return nil, err
	}
	// incoming synapses: a <-[r:SYNAPTIC]- b
	if err := collectEdges(id, true, &out, seen); err != nil {
		return nil, err
	}
	return out, nil
}

func collectEdges(id string, reverse bool, out *[]map[string]any, seen map[string]bool) error {
	var q string
	if reverse {
		q = "MATCH (b:Leaf)-[r:SYNAPTIC]->(a:Leaf {id:$id}) RETURN b.id, b.text, r.type"
	} else {
		q = "MATCH (a:Leaf {id:$id})-[r:SYNAPTIC]->(b:Leaf) RETURN b.id, b.text, r.type"
	}
	stmt, err := conn.Prepare(q)
	if err != nil {
		return err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"id": id})
	if err != nil {
		return err
	}
	defer res.Close()
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			continue
		}
		nid := fmt.Sprint(vals[0])
		if seen[nid] {
			continue
		}
		seen[nid] = true
		typ := "synapse"
		if len(vals) >= 3 && vals[2] != nil {
			typ = fmt.Sprint(vals[2])
		}
		*out = append(*out, map[string]any{"id": nid, "text": fmt.Sprint(vals[1]), "type": typ})
	}
	return nil
}

// pathBetween finds the shortest synapse path from->to. Ladybug's recursive
// relationship query returns the path as a RecursiveRelationship; we map it to
// an ordered list of leaf ids.
func pathBetween(from, to string, max int) ([]string, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	if from == to {
		return []string{from}, nil
	}
	q := fmt.Sprintf(
		"MATCH (a:Leaf {id:$from})-[r:SYNAPTIC* ALL SHORTEST 1..%d]->(b:Leaf {id:$to}) RETURN r",
		max,
	)
	stmt, err := conn.Prepare(q)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"from": from, "to": to})
	if err != nil {
		return nil, err
	}
	defer res.Close()
	if !res.HasNext() {
		return nil, fmt.Errorf("no path %s → %s (max %d)", from, to, max)
	}
	row, err := res.Next()
	if err != nil {
		return nil, err
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) < 1 {
		return nil, fmt.Errorf("path row")
	}
	rr, ok := vals[0].(lbug.RecursiveRelationship)
	if !ok {
		return nil, fmt.Errorf("path row is not a recursive relationship")
	}
	// Ladybug exposes only the *intermediate* nodes on a RecursiveRelationship
	// (the endpoints come from the MATCH anchors). Rebuild the full ordered
	// path as [from, intermediates..., to].
	out := make([]string, 0, len(rr.Nodes)+2)
	out = append(out, from)
	for _, n := range rr.Nodes {
		out = append(out, fmt.Sprint(n.Properties["id"]))
	}
	out = append(out, to)
	return out, nil
}

func rowsToLeafs(res *lbug.QueryResult) []map[string]any {
	var out []map[string]any
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return out
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 5 {
			continue
		}
		lf := map[string]any{
			"id":     fmt.Sprint(vals[0]),
			"text":   fmt.Sprint(vals[1]),
			"root":   fmt.Sprint(vals[2]),
			"type":   fmt.Sprint(vals[3]),
			"source": fmt.Sprint(vals[4]),
		}
		if len(vals) >= 6 {
			lf["confidence"] = fmt.Sprint(vals[5])
		}
		out = append(out, lf)
	}
	return out
}

// HTTP methods exposed to pkg/httpapi (Synapse Matrix surface).

func (HTTP) Leafs(_ context.Context, root, typ, source, text string, limit int) ([]byte, error) {
	leafs, err := queryLeafs(root, typ, source, text, limit)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"count": len(leafs), "leafs": leafs})
}

func (HTTP) Edges(_ context.Context, id string) ([]byte, error) {
	edges, err := queryEdges(id)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"id": id, "count": len(edges), "edges": edges})
}

func (HTTP) Path(_ context.Context, from, to string, max int) ([]byte, error) {
	path, err := pathBetween(from, to, max)
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"from": from, "to": to, "max": max, "path": path})
}

func (HTTP) AddEdge(_ context.Context, body []byte) ([]byte, error) {
	var in struct {
		From string `json:"from"`
		To   string `json:"to"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		return nil, fmt.Errorf("addedge: %w", err)
	}
	from, to := strings.TrimSpace(in.From), strings.TrimSpace(in.To)
	if from == "" || to == "" {
		return nil, fmt.Errorf("addedge: from and to required")
	}
	typ := strings.TrimSpace(in.Type)
	if typ == "" {
		typ = "synapse"
	}
	ingestMu.Lock()
	defer ingestMu.Unlock()
	brainMu.Lock()
	defer brainMu.Unlock()
	if err := addEdgeWriteLocked(dbPath(), from, to, typ); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"from": from, "to": to, "type": typ})
}

// addEdgeWriteLocked closes the serve read handle before opening kb.lbug
// writable (Ladybug dies on same-file double-open), MERGEs the synapse, then
// reopens the read connection. Caller holds brainMu.Lock.
func addEdgeWriteLocked(dbpath, from, to, typ string) error {
	closeBrainLocked()
	wdb, wconn, err := OpenWritable(dbpath)
	if err != nil {
		_ = refreshBrainLocked()
		return err
	}
	defer func() {
		if wconn != nil {
			wconn.Close()
		}
		if wdb != nil {
			wdb.Close()
		}
		_ = refreshBrainLocked()
	}()
	if err := InitSchema(wconn); err != nil {
		return err
	}
	// MERGE guarantees idempotency; SET stamps the synapse label. Both leafs
	// must exist — MATCH with no row MERGEs nothing.
	if err := execParams(wconn,
		`MATCH (a:Leaf {id:$from}), (b:Leaf {id:$to}) MERGE (a)-[r:SYNAPTIC]->(b) SET r.type=$type`,
		map[string]any{"from": from, "to": to, "type": typ},
	); err != nil {
		return err
	}
	return EnsureIndexes(wconn)
}
