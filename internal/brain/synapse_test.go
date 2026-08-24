//go:build cgo && system_ladybug

package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eSlider/2dph/pkg/httpapi"
)

// withTempBrain points the whole read+write brain stack at a temp fixture and
// restores the production path after the test.
func withTempBrain(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kb.lbug")
	prev := dbPathFn
	dbPathFn = func() string { return dbpath }
	t.Cleanup(func() { dbPathFn = prev })
	seedTempBrain(t, dbpath)
	return dbpath
}

// seedTempBrain opens a writable temp DB, creates the schema and a small
// fixture graph (leafs a/b/c/d + synapses a→b, b→c).
func seedTempBrain(t *testing.T, dbpath string) {
	t.Helper()
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}
	ids, err := AddLeafs(conn, []LeafInput{
		{Text: "first neuron a", Source: "pc-agent", Root: "facts", Confidence: "confirmed", Type: "fact"},
		{Text: "second neuron b", Source: "pc-agent", Root: "facts", Confidence: "confirmed", Type: "fact"},
		{Text: "third neuron c", Source: "pc-agent", Root: "info", Confidence: "partial", Type: "reference"},
		{Text: "fourth neuron d", Source: "corpus", Root: "info", Confidence: "hypothesis", Type: "reference"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 4 {
		t.Fatalf("seeded %d leafs, want 4", len(ids))
	}
	if err := EnsureIndexes(conn); err != nil {
		t.Fatal(err)
	}
}

// fixtureIDs returns the ids of leafs whose text matches each given text
// exactly. FTS with a shared token (all fixtures contain "neuron") would
// collapse distinct leafs to the same top hit, so we scan and match exactly.
func fixtureIDs(t *testing.T, texts ...string) []string {
	t.Helper()
	all, err := queryLeafs("", "", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	byText := map[string]string{}
	for _, lf := range all {
		byText[lf["text"].(string)] = lf["id"].(string)
	}
	out := make([]string, 0, len(texts))
	for _, text := range texts {
		id, ok := byText[text]
		if !ok {
			t.Fatalf("fixture leaf %q not found", text)
		}
		out = append(out, id)
	}
	return out
}

// connectRead opens the temp DB as the process read handle so queryLeafs and
// friends (which read the global conn) see the fixture.
func connectRead(t *testing.T) {
	t.Helper()
	if err := openBrain(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeBrain)
}

func TestQueryLeafsByFilters(t *testing.T) {
	withTempBrain(t)
	connectRead(t)

	got, err := queryLeafs("facts", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("facts root = %d leafs, want 2", len(got))
	}
	for _, lf := range got {
		if lf["root"] != "facts" {
			t.Fatalf("wrong root: %v", lf["root"])
		}
	}

	got, err = queryLeafs("", "reference", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("type=reference = %d leafs, want 2", len(got))
	}
	for _, lf := range got {
		if lf["type"] != "reference" {
			t.Fatalf("wrong type: %v", lf["type"])
		}
	}

	got, err = queryLeafs("", "", "pc-agent", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("source=pc-agent = %d leafs, want 3", len(got))
	}
	for _, lf := range got {
		if lf["source"] != "pc-agent" {
			t.Fatalf("wrong source: %v", lf["source"])
		}
	}

	// combined filters
	got, err = queryLeafs("facts", "fact", "pc-agent", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("combined = %d leafs, want 2", len(got))
	}

	// text search across the FTS index
	got, err = queryLeafs("", "", "", "neuron", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatalf("text search returned nothing")
	}
}

func TestAddEdgeAndAdjacency(t *testing.T) {
	dbpath := withTempBrain(t)
	connectRead(t)

	// resolve fixture ids by exact text (FTS with a shared token would collapse
	// every "neuron" leaf to the same top hit)
	ids := fixtureIDs(t, "first neuron a", "second neuron b", "third neuron c")
	a, b, c := ids[0], ids[1], ids[2]

	if err := addEdgeWriteLocked(dbpath, a, b, "supports"); err != nil {
		t.Fatal(err)
	}
	if err := addEdgeWriteLocked(dbpath, b, c, "supports"); err != nil {
		t.Fatal(err)
	}
	// re-adding the same edge is idempotent (MERGE)
	if err := addEdgeWriteLocked(dbpath, a, b, "supports"); err != nil {
		t.Fatal(err)
	}

	adj, err := queryEdges(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(adj) != 1 || adj[0]["id"] != b {
		t.Fatalf("adjacency of a = %v, want [%s]", adj, b)
	}
	if adj[0]["type"] != "supports" {
		t.Fatalf("edge type = %v, want supports", adj[0]["type"])
	}

	// b has a→b (incoming) and b→c (outgoing)
	adj, err = queryEdges(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(adj) != 2 {
		t.Fatalf("adjacency of b = %d edges, want 2", len(adj))
	}

	// c has only b→c incoming
	adj, err = queryEdges(c)
	if err != nil {
		t.Fatal(err)
	}
	if len(adj) != 1 || adj[0]["id"] != b {
		t.Fatalf("adjacency of c = %v, want [%s]", adj, b)
	}
}

func TestPathBetween(t *testing.T) {
	dbpath := withTempBrain(t)
	connectRead(t)

	ids := fixtureIDs(t, "first neuron a", "second neuron b", "third neuron c")
	a, b, c := ids[0], ids[1], ids[2]

	if err := addEdgeWriteLocked(dbpath, a, b, "synapse"); err != nil {
		t.Fatal(err)
	}
	if err := addEdgeWriteLocked(dbpath, b, c, "synapse"); err != nil {
		t.Fatal(err)
	}

	path, err := pathBetween(a, c, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 3 || path[0] != a || path[2] != c {
		t.Fatalf("path = %v, want [%s %s %s]", path, a, b, c)
	}
	if path[1] != b {
		t.Fatalf("middle node = %v, want %s", path[1], b)
	}

	// same node is a trivial path
	path, err = pathBetween(a, a, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 1 || path[0] != a {
		t.Fatalf("self path = %v", path)
	}

	// disconnected: a and d share no synapse
	did := fixtureIDs(t, "fourth neuron d")[0]
	if _, err := pathBetween(a, did, 6); err == nil {
		t.Fatal("expected no path between disconnected leafs")
	}
}

func TestHTTPLeafsEdgesPathJSON(t *testing.T) {
	withTempBrain(t)
	connectRead(t)

	h := HTTP{}
	// Leafs
	body, err := h.Leafs(context.Background(), "facts", "", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("leafs not json: %v (%s)", err, body)
	}
	if out.Count != 2 {
		t.Fatalf("leafs count = %d, want 2", out.Count)
	}

	// AddEdge then Edges then Path through the HTTP surface
	ids := fixtureIDs(t, "first neuron a", "second neuron b", "third neuron c")
	a, b, c := ids[0], ids[1], ids[2]
	if _, err := h.AddEdge(context.Background(), []byte(`{"from":"`+a+`","to":"`+b+`","type":"supports"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.AddEdge(context.Background(), []byte(`{"from":"`+b+`","to":"`+c+`","type":"supports"}`)); err != nil {
		t.Fatal(err)
	}
	edgeBody, err := h.Edges(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(edgeBody) {
		t.Fatalf("edges not json: %s", edgeBody)
	}
	pathBody, err := h.Path(context.Background(), a, c, 6)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(pathBody) {
		t.Fatalf("path not json: %s", pathBody)
	}
	if _, err := h.AddEdge(context.Background(), []byte(`{"to":"b"}`)); err == nil {
		t.Fatal("AddEdge must reject missing from")
	}
}

// TestSynapseHTTPServerEndToEnd boots the real httpapi.Server against a temp
// brain with token auth and drives it with real HTTP requests (curl-equivalent).
// This proves routing + auth + the brain surface all the way to the wire,
// offline, without touching the production kb.lbug.
func TestSynapseHTTPServerEndToEnd(t *testing.T) {
	withTempBrain(t)
	connectRead(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: httpapi.NewServer(HTTP{}, 2).SetToken("sekrit")}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close() })

	addr := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 10 * time.Second}
	get := func(path, token string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, addr+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}
	post := func(path, body, token string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, addr+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, out
	}

	// no token -> 401 on a data route, but /health stays open
	if code, _ := get("/leafs?root=facts", ""); code != http.StatusUnauthorized {
		t.Fatalf("no-token /leafs = %d, want 401", code)
	}
	if code, _ := get("/health", ""); code != http.StatusOK {
		t.Fatalf("no-token /health = %d, want 200", code)
	}

	// leafs by root
	code, body := get("/leafs?root=facts&n=10", "sekrit")
	if code != http.StatusOK || !strings.Contains(string(body), `"count":2`) {
		t.Fatalf("leafs root=facts code=%d body=%s", code, body)
	}
	// leafs by source (pc-agent)
	code, body = get("/leafs?source=pc-agent", "sekrit")
	if code != http.StatusOK || !strings.Contains(string(body), `"count":3`) {
		t.Fatalf("leafs source=pc-agent code=%d body=%s", code, body)
	}

	// build a chain a->b->c and assert path
	fids := fixtureIDs(t, "first neuron a", "second neuron b", "third neuron c")
	a, b, c := fids[0], fids[1], fids[2]
	edge := func(from, to string) {
		t.Helper()
		payload := fmt.Sprintf(`{"from":%q,"to":%q,"type":"supports"}`, from, to)
		code, body := post("/addedge", payload, "sekrit")
		if code != http.StatusOK {
			t.Fatalf("addedge code=%d body=%s", code, body)
		}
	}
	edge(a, b)
	edge(b, c)

	code, body = get("/edges?id="+a, "sekrit")
	if code != http.StatusOK || !strings.Contains(string(body), `"count":1`) {
		t.Fatalf("edges(a) code=%d body=%s", code, body)
	}

	code, body = get("/path?from="+a+"&to="+c+"&max=6", "sekrit")
	if code != http.StatusOK || !strings.Contains(string(body), b) {
		t.Fatalf("path(a,c) code=%d body=%s", code, body)
	}

	// openapi documents the new surface
	code, body = get("/openapi.json", "sekrit")
	if code != http.StatusOK || !strings.Contains(string(body), `"/leafs"`) ||
		!strings.Contains(string(body), `"/addedge"`) || !strings.Contains(string(body), `"/path"`) {
		t.Fatalf("openapi code=%d body=%s", code, body)
	}
}

