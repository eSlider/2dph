//go:build cgo && system_ladybug

package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
	"github.com/eSlider/2dph/internal/brain/rank"
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/pkg/cli"
)

const defaultPort = 17830
const daemonPath = "/embed"
const healthPath = "/health"

func runSearch(args []string) int {
	opt, err := rank.ParseArgs(args)
	if err != nil {
		if errors.Is(err, cli.ErrHelp) {
			return 0
		}
		fmt.Fprintf(os.Stderr, "brain/search: %v\n%s\n", err, rank.Usage)
		return 2
	}
	root, repo, limit, query := opt.Root, opt.Repo, opt.Limit, opt.Query
	jsonOut := opt.JSONOut

	if opt.ListModel {
		dir, err := modelDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(dir)
		return 0
	}

	if err := openBrain(); err != nil {
		fmt.Fprintf(os.Stderr, "open brain: %v\n", err)
		return 1
	}
	defer closeBrain()

	hits, err := searchHits(query, root, repo, limit, opt.AsOf, opt.SortDate, opt.SortDesc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		return 1
	}
	if opt.Hop > 0 {
		if err := attachHops(hits, opt.Hop); err != nil {
			fmt.Fprintf(os.Stderr, "hop: %v\n", err)
			return 1
		}
	}

	results := hits
	for i := range results {
		if results[i].Text != "" {
			runes := []rune(results[i].Text)
			if len(runes) > 280 {
				runes = runes[:280]
			}
			results[i].Snippet = string(runes)
		}
	}

	webOut := rank.Deduce(results, query, root, opt.NoWeb, func(q string) rank.SecondSource {
		return lookupWeb(context.Background(), q)
	})

	out := Dict{
		{"query", query},
		{"root_filter", root},
		{"as_of", opt.AsOf},
		{"count", len(results)},
		{"results", resultsToDicts(results)},
	}
	if webOut != nil {
		out = append(out, KV{"web", secondToDict(*webOut)})
	}

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return b2i(enc.Encode(toJSONOut(results, query, root, opt.AsOf, webOut)))
	}
	fmt.Print(toYAML(out, 0))
	return 0
}

func searchHits(query, root, repo string, limit int, asOf string, sortDate, sortDesc bool) ([]Hit, error) {
	emb, err := embedQuery(query)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	fts, err := queryFTS(query, limit*3)
	if err != nil {
		return nil, fmt.Errorf("fts: %w", err)
	}
	var vec []Hit
	if vec, err = queryVector(emb, limit*3); err != nil {
		fmt.Fprintf(os.Stderr, "vec: %v\n", err)
	}
	return rank.RankAndFilterSort(fts, vec, root, repo, asOf, limit, sortDate, sortDesc), nil
}

func attachHops(hits []Hit, n int) error {
	brainMu.RLock()
	defer brainMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("brain not open")
	}
	for i := range hits {
		var hops []rank.HopNode
		for d := 1; d <= n; d++ {
			stmt, err := conn.Prepare(rank.HopStmt(d))
			if err != nil {
				return err
			}
			res, err := conn.Execute(stmt, map[string]any{"id": hits[i].ID})
			stmt.Close()
			if err != nil {
				return err
			}
			for res.HasNext() {
				row, err := res.Next()
				if err != nil {
					res.Close()
					return err
				}
				vals, err := row.GetAsSlice()
				if err != nil || len(vals) < 3 {
					continue
				}
				hops = append(hops, rank.HopNode{
					ID:    fmt.Sprint(vals[0]),
					Label: rank.HopLabel(d),
					Name:  fmt.Sprint(vals[1]),
					Depth: int(asInt(vals[2])),
				})
			}
			res.Close()
		}
		hits[i].Hops = hops
	}
	return nil
}

func b2i(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

func queryFTS(text string, limit int) ([]Hit, error) {
	brainMu.RLock()
	defer brainMu.RUnlock()
	stmt, err := conn.Prepare(rank.FTSStmt)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"q": text, "n": limit})
	if err != nil {
		return nil, err
	}
	defer res.Close()
	return rowsToHits(res)
}

func queryVector(emb []float64, limit int) ([]Hit, error) {
	// ANN path (issue #204): top-k from the incremental HNSW index, then
	// metadata via point lookups. Falls back to the brute-force scan when
	// the index is absent/corrupt/disabled — search never fails because of
	// ANN.
	if annEnabled() {
		if hits, err := annQueryVector(emb, limit); err != nil {
			fmt.Fprintf(os.Stderr, "ann: %v\n", err)
		} else if len(hits) > 0 {
			return hits, nil
		}
	}
	brainMu.RLock()
	defer brainMu.RUnlock()
	stmt, err := conn.Prepare(vecScanStmt)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, nil)
	if err != nil {
		return nil, err
	}
	defer res.Close()

	type scored struct {
		hit Hit
		sim float64
	}
	var all []scored
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 8 {
			continue
		}
		e, ok := vals[7].([]any)
		if !ok || len(e) == 0 {
			continue
		}
		sim := cosineToQuery(emb, e)
		all = append(all, scored{hit: Hit{
			ID: fmt.Sprint(vals[0]), Text: fmt.Sprint(vals[1]),
			Root: fmt.Sprint(vals[2]), Source: fmt.Sprint(vals[3]),
			Confidence: fmt.Sprint(vals[4]), ValidFrom: nullStr(vals[5]),
			ValidTo: nullStr(vals[6]), Score: sim,
		}, sim: sim})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].sim > all[j].sim })
	if len(all) > limit {
		all = all[:limit]
	}
	out := make([]Hit, 0, len(all))
	for _, s := range all {
		out = append(out, s.hit)
	}
	return out, nil
}

// vecScanStmt loads every leaf embedding plus the fields fused into search
// results. It replaces the liblbug HNSW QUERY_VECTOR_INDEX (Leaf_vec), which
// SIGSEGVs in liblbug 0.19.0 once the graph grows past ~1300 nodes
// (simsimd_cos_f32 NULL deref during HNSW insert). A linear scan is fast at
// our scale and crash-free.
const vecScanStmt = "MATCH (l:Leaf) RETURN l.id, l.text, l.root, l.source, " +
	"l.confidence, l.valid_from, l.valid_to, l.embedding"

// cosineToQuery computes cosine similarity between the query embedding
// ([]float64, from the embedding daemon) and a stored embedding ([]any of
// float32, how liblbug surfaces FLOAT[256] columns). A zero-norm side yields
// 0 rather than NaN.
func cosineToQuery(q []float64, e []any) float64 {
	n := len(q)
	if len(e) < n {
		n = len(e)
	}
	if n == 0 {
		return 0
	}
	var dot, nq, ne float64
	for i := 0; i < n; i++ {
		f, ok := e[i].(float32)
		if !ok {
			fv, ok2 := e[i].(float64)
			if !ok2 {
				return 0
			}
			f = float32(fv)
		}
		qv := q[i]
		dot += qv * float64(f)
		nq += qv * qv
		ne += float64(f) * float64(f)
	}
	if nq == 0 || ne == 0 {
		return 0
	}
	return dot / (sqrt64(nq) * sqrt64(ne))
}

// sqrt64 is a tiny inline wrapper so the hot loop stays dependency-free.
func sqrt64(x float64) float64 {
	if x <= 0 {
		return 0
	}
	return math.Sqrt(x)
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
		conf := ""
		if len(vals) >= 6 {
			conf = fmt.Sprint(vals[5])
		}
		vf, vt := "", ""
		if len(vals) >= 8 {
			vf = nullStr(vals[6])
			vt = nullStr(vals[7])
		}
		hits = append(hits, Hit{
			ID: id, Text: text, Root: root, Source: source, Score: score,
			Confidence: conf, ValidFrom: vf, ValidTo: vt,
		})
	}
	return hits, nil
}

func nullStr(v any) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprint(v)
	if s == "<nil>" {
		return ""
	}
	return s
}

// JSON output types
type jsonOut struct {
	Query      string             `json:"query"`
	RootFilter string             `json:"root_filter"`
	AsOf       string             `json:"as_of,omitempty"`
	Count      int                `json:"count"`
	Results    []jsonHit          `json:"results"`
	Web        *rank.SecondSource `json:"web,omitempty"`
}

type jsonHit struct {
	ID         string         `json:"id"`
	Text       string         `json:"text"`
	Root       string         `json:"root"`
	Confidence string         `json:"confidence,omitempty"`
	Score      float64        `json:"score"`
	Snippet    string         `json:"snippet,omitempty"`
	ValidFrom  string         `json:"valid_from,omitempty"`
	ValidTo    string         `json:"valid_to,omitempty"`
	Hops       []rank.HopNode `json:"hops,omitempty"`
}

func toJSONOut(hits []Hit, query, rootFilter, asOf string, web *rank.SecondSource) *jsonOut {
	out := make([]jsonHit, len(hits))
	for i, h := range hits {
		out[i] = jsonHit{
			ID:         h.ID,
			Text:       h.Text,
			Root:       h.Root,
			Confidence: h.Confidence,
			Score:      h.Score,
			Snippet:    h.Snippet,
			ValidFrom:  h.ValidFrom,
			ValidTo:    h.ValidTo,
			Hops:       h.Hops,
		}
	}
	return &jsonOut{
		Query:      query,
		RootFilter: rootFilter,
		AsOf:       asOf,
		Count:      len(hits),
		Results:    out,
		Web:        web,
	}
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
		if h.Confidence != "" {
			d = append(d, KV{"confidence", h.Confidence})
		}
		if h.ValidFrom != "" {
			d = append(d, KV{"valid_from", h.ValidFrom})
		}
		if h.ValidTo != "" {
			d = append(d, KV{"valid_to", h.ValidTo})
		}
		if h.Snippet != "" {
			d = append(d, KV{"snippet", h.Snippet})
		}
		if len(h.Hops) > 0 {
			nodes := make([]any, len(h.Hops))
			for j, n := range h.Hops {
				nodes[j] = Dict{
					{"id", n.ID},
					{"label", n.Label},
					{"name", n.Name},
					{"depth", n.Depth},
				}
			}
			d = append(d, KV{"hops", nodes})
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
	log.Printf("brain search daemon listening on %s", addr)
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
	port := brainCfg().SearchDaemonPort
	if port <= 0 {
		port = defaultPort
	}
	if emb, err := tryDaemon(text, port); err == nil {
		return emb, nil
	}
	if !brainCfg().SearchNoDaemon {
		if err := ensureDaemon(port); err == nil {
			if emb, err := tryDaemon(text, port); err == nil {
				return emb, nil
			}
		}
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
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

// Main is the bin/brain/search.go entry: search, serve, or --list-model.
func Main(args []string) int {
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/search: config: %v\n", err)
		return 1
	}
	Configure(cfg)
	if len(args) > 0 && args[0] == "serve" {
		port := defaultPort
		if len(args) > 1 {
			if p, err := strconv.Atoi(args[1]); err == nil {
				port = p
			}
		}
		if err := serve(port); err != nil {
			log.Printf("brain/search serve: %v", err)
			return 1
		}
		return 0
	}
	return runSearch(args)
}
