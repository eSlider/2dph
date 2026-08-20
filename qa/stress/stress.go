// usr/bin/env go run "$0" "$@"; exit
//
// qa/stress.go - concurrent load generator for the live brain (REST surface).
//
//	BRAIN_URL=http://127.0.0.1:8630 ./qa/stress.go --c 8 --d 30 --json
//	./qa/stress.go --mix search:6,get:2,stats:1,audit:1 --q "oo catalog" --json
//
// Read-only by default (search/get/stats/audit). Ingests are never sent.
// Gates: health < 500ms, search p95 < 1000ms, per-type error rate < 1%.
// Exit 1 if any gate fails. Writes nothing to the brain or the repo.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cliparse "github.com/eSlider/2dph/internal/cli"
)

const (
	defaultStressBrain = "http://127.0.0.1:8630"
	defaultMix      = "search:6,get:2,stats:1,audit:1"
	gateHealthMS    = 500.0
	gateSearchP95MS = 1000.0
	gateGetP95MS    = 200.0
	gateErrPct      = 1.0
)

type querySpec struct {
	Q    string `json:"q"`
	Root string `json:"root,omitempty"`
}

var builtinQueries = []querySpec{
	{Q: "docker"},
	{Q: "compose"},
	{Q: "ssh config"},
	{Q: "oo catalog merge"},
	{Q: "kubernetes"},
	{Q: "container orchestration"},
	{Q: "observability"},
	{Q: "LadybugDB"},
	{Q: "potion"},
	{Q: "service"},
	{Q: "configured", Root: "facts"},
	{Q: "running", Root: "facts"},
	{Q: "docker", Root: "info"},
}

type dist struct {
	N     int     `json:"n"`
	MinMS float64 `json:"min_ms"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
	AvgMS float64 `json:"avg_ms"`
}

func stats(samples []float64) dist {
	n := len(samples)
	if n == 0 {
		return dist{}
	}
	s := append([]float64(nil), samples...)
	sort.Float64s(s)
	sum := 0.0
	for _, v := range s {
		sum += v
	}
	p := func(q float64) float64 {
		i := int(float64(n-1) * q)
		return round1(s[i])
	}
	return dist{
		N:     n,
		MinMS: round1(s[0]),
		P50MS: p(0.50),
		P95MS: p(0.95),
		P99MS: p(0.99),
		MaxMS: round1(s[n-1]),
		AvgMS: round1(sum / float64(n)),
	}
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10.0
}

type endpointStats struct {
	Dist    dist  `json:"dist"`
	OK      int   `json:"ok"`
	Errors  int   `json:"errors"`
	ErrPct  float64 `json:"err_pct"`
	Throughput float64 `json:"req_per_sec"`
}

func (e *endpointStats) add(ms float64, ok bool) {
	if ok {
		e.OK++
	} else {
		e.Errors++
	}
}

type report struct {
	Brain     string                   `json:"brain"`
	Concurrency int                    `json:"concurrency"`
	DurationS float64                  `json:"duration_s"`
	Total     int                      `json:"total"`
	TotalReqPS float64                 `json:"total_req_per_sec"`
	OK        bool                     `json:"ok"`
	Gates     map[string]bool          `json:"gates"`
	Endpoints map[string]endpointStats `json:"endpoints"`
}

func req(method, urlStr string, timeout time.Duration) (int, error) {
	r, err := http.NewRequest(method, urlStr, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(r)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func searchURL(brain string, q querySpec, n int, noWeb bool) string {
	u, _ := url.Parse(strings.TrimRight(brain, "/") + "/search")
	values := u.Query()
	values.Set("q", q.Q)
	values.Set("n", strconv.Itoa(n))
	if q.Root != "" {
		values.Set("root", q.Root)
	}
	if noWeb {
		values.Set("noweb", "1")
	}
	u.RawQuery = values.Encode()
	return u.String()
}

func parseMix(spec string) map[string]int {
	out := map[string]int{}
	for _, part := range strings.Split(spec, ",") {
		kv := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(kv) != 2 {
			continue
		}
		if w, err := strconv.Atoi(strings.TrimSpace(kv[1])); err == nil && w > 0 {
			out[strings.TrimSpace(kv[0])] = w
		}
	}
	if len(out) == 0 {
		out["search"] = 1
	}
	return out
}

func weightedPick(rng *rand.Rand, mix map[string]int) string {
	total := 0
	for _, w := range mix {
		total += w
	}
	roll := rng.Intn(total)
	for name, w := range mix {
		if roll < w {
			return name
		}
		roll -= w
	}
	return "search"
}

func run(c cfg) (*report, error) {
	rep := &report{
		Brain:        strings.TrimRight(c.brain, "/"),
		Concurrency:  c.concurrency,
		OK:           true,
		Gates:        map[string]bool{},
		Endpoints:    map[string]endpointStats{},
	}
	mix := parseMix(c.mix)
	queries := append([]querySpec(nil), builtinQueries...)
	for _, q := range c.queries {
		queries = append(queries, querySpec{Q: q})
	}
	if len(queries) == 0 {
		queries = builtinQueries
	}

	healthMS, code, err := timedStatus(func() (int, error) { return req(http.MethodGet, rep.Brain+"/health", 5*time.Second) })
	rep.Gates["health"] = err == nil && code == 200 && healthMS <= gateHealthMS
	if !rep.Gates["health"] {
		rep.OK = false
	}

	leafID := ""
	if code, _ := req(http.MethodGet, searchURL(rep.Brain, querySpec{Q: "docker"}, 1, true), 10*time.Second); code == 200 {
		body, _ := httpGetBody(rep.Brain + "/search?q=docker&n=1")
		var out struct {
			Results []struct {
				ID string `json:"id"`
			} `json:"results"`
		}
		_ = json.Unmarshal(body, &out)
		if len(out.Results) > 0 {
			leafID = out.Results[0].ID
		}
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	end := time.Now().Add(time.Duration(c.duration) * time.Second)

	var mu sync.Mutex
	var wg sync.WaitGroup
	var total int
	samples := map[string][]float64{}
	t0 := time.Now()

	for i := 0; i < c.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(end) {
				name := weightedPick(rng, mix)
				var ms float64
				var ok bool
				var code int
				switch name {
				case "search":
					q := queries[rng.Intn(len(queries))]
					start := time.Now()
					code, err = req(http.MethodGet, searchURL(rep.Brain, q, 5, !c.web), 30*time.Second)
					ms = float64(time.Since(start).Microseconds()) / 1000.0
					ok = err == nil && code == 200
				case "get":
					start := time.Now()
					code, err = req(http.MethodGet, rep.Brain+"/get?id="+url.QueryEscape(leafID)+"&body=1", 10*time.Second)
					ms = float64(time.Since(start).Microseconds()) / 1000.0
					ok = err == nil && code == 200 && leafID != ""
				case "stats":
					start := time.Now()
					code, err = req(http.MethodGet, rep.Brain+"/stats", 10*time.Second)
					ms = float64(time.Since(start).Microseconds()) / 1000.0
					ok = err == nil && code == 200
				case "audit":
					start := time.Now()
					code, err = req(http.MethodGet, rep.Brain+"/audit", 10*time.Second)
					ms = float64(time.Since(start).Microseconds()) / 1000.0
					ok = err == nil && code == 200
				default:
					continue
				}
				mu.Lock()
				total++
				if _, exists := samples[name]; !exists {
					samples[name] = []float64{}
				}
				samples[name] = append(samples[name], ms)
				st := rep.Endpoints[name]
				st.add(ms, ok)
				rep.Endpoints[name] = st
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	rep.DurationS = round1(time.Since(t0).Seconds())
	rep.Total = total
	rep.TotalReqPS = round1(float64(total) / rep.DurationS)

	for name, ms := range samples {
		st := rep.Endpoints[name]
		st.Dist = stats(ms)
		if st.OK+st.Errors > 0 {
			st.ErrPct = round1(float64(st.Errors) / float64(st.OK+st.Errors) * 100.0)
		}
		st.Throughput = round1(float64(st.OK+st.Errors) / rep.DurationS)
		rep.Endpoints[name] = st
	}

	rep.Gates["search_p95"] = true
	if st, ok := rep.Endpoints["search"]; ok {
		rep.Gates["search_p95"] = st.Dist.N == 0 || st.Dist.P95MS <= gateSearchP95MS
	}
	rep.Gates["get_p95"] = true
	if st, ok := rep.Endpoints["get"]; ok {
		rep.Gates["get_p95"] = st.Dist.N == 0 || st.Dist.P95MS <= gateGetP95MS
	}
	for _, st := range rep.Endpoints {
		if st.ErrPct > gateErrPct {
			rep.OK = false
		}
	}
	for _, g := range rep.Gates {
		if !g {
			rep.OK = false
		}
	}
	return rep, nil
}

func timedStatus(fn func() (int, error)) (float64, int, error) {
	start := time.Now()
	code, err := fn()
	return float64(time.Since(start).Microseconds()) / 1000.0, code, err
}

func httpGetBody(urlStr string) ([]byte, error) {
	r, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

type cfg struct {
	brain, mix    string
	concurrency   int
	duration      int
	queries       []string
	web           bool
	jsonOut       bool
}

func parseFlags(args []string) (cfg, error) {
	c := cfg{brain: defaultStressBrain, mix: defaultMix, concurrency: 8, duration: 20}
	if v := os.Getenv("BRAIN_URL"); v != "" {
		c.brain = v
	}
	p := cliparse.New("stress")
	p.Description = "2dph brain concurrent load generator (read-only)"
	p.String(&c.brain, "", "brain", "brain base URL")
	p.Int(&c.concurrency, "", "c", "concurrent workers")
	p.Int(&c.duration, "", "d", "load duration (seconds)")
	p.String(&c.mix, "", "mix", "endpoint weights, e.g. search:6,get:2,stats:1,audit:1")
	p.StringSlice(&c.queries, "", "q", "extra search query (repeatable)")
	p.Bool(&c.web, "", "web", "allow the web second source (D17) for low-match queries")
	p.Bool(&c.jsonOut, "", "json", "print JSON report")
	if err := cliparse.Parse(p, args); err != nil {
		return c, err
	}
	return c, nil
}

func main() {
	c, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "stress:", err)
		os.Exit(1)
	}
	rep, err := run(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stress:", err)
		os.Exit(1)
	}
	if c.jsonOut {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("ok=%v brain=%s c=%d d=%ds total=%d %.0f req/s\n",
			rep.OK, rep.Brain, rep.Concurrency, int(rep.DurationS), rep.Total, rep.TotalReqPS)
		for name, st := range rep.Endpoints {
			fmt.Printf("  %s: n=%d ok=%d err=%d (%.1f%%) p50=%.1f p95=%.1f p99=%.1f max=%.1fms %.0f req/s\n",
				name, st.Dist.N, st.OK, st.Errors, st.ErrPct, st.Dist.P50MS, st.Dist.P95MS, st.Dist.P99MS, st.Dist.MaxMS, st.Throughput)
		}
		for k, v := range rep.Gates {
			fmt.Printf("  gate %s: %v\n", k, v)
		}
	}
	if !rep.OK {
		os.Exit(1)
	}
}