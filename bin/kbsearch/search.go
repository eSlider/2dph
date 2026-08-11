// Hybrid FTS + vector search implementation, plus daemon client/server.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
)

const defaultPort = 17830
const daemonPath = "/embed"
const healthPath = "/health"

func runSearch(args []string) int {
	// Manual flag parsing to allow flags after query (like Python argparse)
	root := ""
	repo := ""
	limit := 20
	jsonOut := false
	listModel := false

	var queryArgs []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--root":
			if i+1 < len(args) {
				root = args[i+1]
				i++
			}
		case "--repo":
			if i+1 < len(args) {
				repo = args[i+1]
				i++
			}
		case "-n":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					limit = n
				}
				i++
			}
		case "--json":
			jsonOut = true
		case "--list-model":
			listModel = true
		default:
			if !strings.HasPrefix(args[i], "-") {
				queryArgs = append(queryArgs, args[i])
			}
		}
	}

	if listModel {
		dir, err := modelDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(dir)
		return 0
	}

	query := strings.TrimSpace(strings.Join(queryArgs, " "))
	if query == "" {
		fmt.Fprintln(os.Stderr, "usage: kbsearch \"query\" [--root facts|info] [--repo REPO] [-n N] [--json]")
		return 1
	}

	if err := openBrain(); err != nil {
		fmt.Fprintf(os.Stderr, "open brain: %v\n", err)
		return 1
	}
	defer closeBrain()

	emb, err := embedQuery(query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "embed: %v\n", err)
		return 1
	}

	fts, err := queryFTS(query, limit*3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fts: %v\n", err)
		return 1
	}

	var vec []Hit
	if vec, err = queryVector(emb, limit*3); err != nil {
		fmt.Fprintf(os.Stderr, "vec: %v\n", err)
	}

	results := hybrid(fts, vec, limit)

	if root != "" {
		results = filterRoot(results, root)
	}
	if repo != "" {
		results = filterRepo(results, repo)
	}
	if len(results) > limit {
		results = results[:limit]
	}

	for i := range results {
		if results[i].Text != "" {
			runes := []rune(results[i].Text)
			if len(runes) > 280 {
				runes = runes[:280]
			}
			results[i].Snippet = string(runes)
		}
	}

	out := Dict{
		{"query", query},
		{"root_filter", root},
		{"count", len(results)},
		{"results", resultsToDicts(results)},
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return b2i(enc.Encode(toJSONOut(results, query, root)))
	}
	fmt.Print(toYAML(out, 0))
	return 0
}

func b2i(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

func queryFTS(text string, limit int) ([]Hit, error) {
	stmt, err := conn.Prepare(
		"CALL QUERY_FTS_INDEX('Leaf', 'id', $q) " +
			"RETURN node.id, node.text, node.root, node.source, score ORDER BY score LIMIT $n",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"q": text, "n": limit})
	if err != nil {
		return nil, err
	}
	return rowsToHits(res)
}

func queryVector(emb []float64, limit int) ([]Hit, error) {
	embList := make([]any, len(emb))
	for i, v := range emb {
		embList[i] = v
	}
	stmt, err := conn.Prepare(
		"CALL QUERY_VECTOR_INDEX('Leaf', 'Leaf_vec', $q, $n) " +
			"RETURN node.id, node.text, node.root, node.source, distance ORDER BY distance LIMIT $n",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"q": embList, "n": limit})
	if err != nil {
		return nil, err
	}
	hits, err := rowsToHits(res)
	if err != nil {
		return nil, err
	}
	for i := range hits {
		hits[i].Score = 1.0 - hits[i].Score
	}
	return hits, nil
}

func rowsToHits(res *lbug.QueryResult) ([]Hit, error) {
	var hits []Hit
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 5 {
			continue
		}
		id := fmt.Sprint(vals[0])
		text := fmt.Sprint(vals[1])
		root := fmt.Sprint(vals[2])
		source := fmt.Sprint(vals[3])
		score := float64(vals[4].(float64))
		hits = append(hits, Hit{ID: id, Text: text, Root: root, Source: source, Score: score})
	}
	return hits, nil
}

// JSON output types
type jsonOut struct {
	Query      string     `json:"query"`
	RootFilter string     `json:"root_filter"`
	Count      int        `json:"count"`
	Results    []jsonHit  `json:"results"`
}

type jsonHit struct {
	ID      string  `json:"id"`
	Text    string  `json:"text"`
	Root    string  `json:"root"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

func toJSONOut(hits []Hit, query, rootFilter string) *jsonOut {
	out := make([]jsonHit, len(hits))
	for i, h := range hits {
		out[i] = jsonHit{
			ID:      h.ID,
			Text:    h.Text,
			Root:    h.Root,
			Score:   h.Score,
			Snippet: h.Snippet,
		}
	}
	return &jsonOut{
		Query:      query,
		RootFilter: rootFilter,
		Count:      len(hits),
		Results:    out,
	}
}

func hybrid(fts, vec []Hit, limit int) []Hit {
	byID := make(map[string]Hit)
	rrf := make(map[string]float64)

	for rank, h := range fts {
		byID[h.ID] = h
		rrf[h.ID] += 1.0 / (60 + float64(rank+1))
	}
	for rank, h := range vec {
		if _, ok := byID[h.ID]; !ok {
			byID[h.ID] = h
		} else {
			existing := byID[h.ID]
			if existing.Score == 0 {
				existing.Score = h.Score
				byID[h.ID] = existing
			}
		}
		rrf[h.ID] += 1.0 / (60 + float64(rank+1))
	}

	type scored struct {
		id  string
		rrf float64
	}
	var scoredList []scored
	for id, v := range rrf {
		scoredList = append(scoredList, scored{id, v})
	}
	sort.Slice(scoredList, func(i, j int) bool {
		return scoredList[i].rrf > scoredList[j].rrf
	})

	var out []Hit
	for i, s := range scoredList {
		if i >= limit {
			break
		}
		h := byID[s.id]
		out = append(out, h)
	}
	return out
}

func filterRoot(hits []Hit, root string) []Hit {
	var out []Hit
	for _, h := range hits {
		if h.Root == root {
			out = append(out, h)
		}
	}
	return out
}

func filterRepo(hits []Hit, repo string) []Hit {
	var out []Hit
	for _, h := range hits {
		if strings.Contains(h.Source, repo) {
			out = append(out, h)
		}
	}
	return out
}

func resultsToDicts(hits []Hit) []any {
	out := make([]any, len(hits))
	for i, h := range hits {
		d := Dict{
			{"id", h.ID},
			{"text", h.Text},
			{"root", h.Root},
			{"score", h.Score},
		}
		if h.Snippet != "" {
			d = append(d, KV{"snippet", h.Snippet})
		}
		out[i] = d
	}
	return out
}

// --- Daemon server ---
func serve(port int) error {
	model, err := loadModel()
	if err != nil {
		return fmt.Errorf("load model: %w", err)
	}
	defer model.Close()

	mux := http.NewServeMux()
	mux.HandleFunc(healthPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc(daemonPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		vec, err := model.Embed(req.Text)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"vector": vec})
	})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	log.Printf("kbsearch daemon listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

// --- Daemon client ---
var daemonClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
	},
}

func embedQuery(text string) ([]float64, error) {
	port := defaultPort
	if envPort := os.Getenv("KBSEARCH_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			port = p
		}
	}
	emb, err := tryDaemon(text, port)
	if err == nil {
		return emb, nil
	}

	model, err := loadModel()
	if err != nil {
		return nil, fmt.Errorf("fallback load model: %w", err)
	}
	defer model.Close()
	return model.Embed(text)
}

func tryDaemon(text string, port int) ([]float64, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, daemonPath)
	payload := map[string]string{"text": text}
	body, _ := json.Marshal(payload)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := daemonClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon HTTP %d", resp.StatusCode)
	}
	var r struct {
		Vector []float64 `json:"vector"`
		Error  string    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Error != "" {
		return nil, errors.New(r.Error)
	}
	return r.Vector, nil
}

func ensureDaemon(port int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, healthPath)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	if resp, err := daemonClient.Do(req); err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "serve", strconv.Itoa(port))
	cmd.Dir, _ = filepath.Split(self)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return err
	}

	for i := 0; i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
		if resp, err := daemonClient.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
	return fmt.Errorf("daemon failed to start on port %d", port)
}