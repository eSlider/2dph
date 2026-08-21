// usr/bin/env go run "$0" "$@"; exit
//
// test/system/system_perf.go - system performance test: PicoClaw surface (brain MCP)
// + optional reasoner.
//
//	BRAIN_URL=http://127.0.0.1:8630 ./test/system/system_perf.go --json
//	REASONER_BASE_URL=http://127.0.0.1:11435/v1 REASONER_MODEL=qwen3.5:9b \
//	  ./test/system/system_perf.go --reasoner --picoclaw --json
//
// Does not write Ladybug. Search includes web (D17); expect ~10s+ per search.
// Exit 1 if health/get/audit gates fail. Reasoner is measured, not gated.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	cliparse "github.com/eSlider/2dph/pkg/cli"
)

const (
	defaultBrain    = "http://127.0.0.1:8630"
	defaultReasoner = "http://127.0.0.1:11435/v1"
	defaultModel    = "qwen3.5:9b"
	defaultPicoClaw = "http://127.0.0.1:18790"

	gateHealthMS   = 500.0
	gateGetP50MS   = 50.0
	gateAuditP50MS = 50.0
)

type dist struct {
	N     int     `json:"n"`
	MinMS float64 `json:"min_ms"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	MaxMS float64 `json:"max_ms"`
	AvgMS float64 `json:"avg_ms"`
}

func stats(samples []float64) dist {
	n := len(samples)
	s := append([]float64(nil), samples...)
	sort.Float64s(s)
	sum := 0.0
	for _, v := range s {
		sum += v
	}
	p95 := s[n-1]
	if i := int(float64(n) * 0.95); i < n {
		p95 = s[i]
	}
	return dist{
		N:     n,
		MinMS: round1(s[0]),
		P50MS: round1(s[n/2]),
		P95MS: round1(p95),
		MaxMS: round1(s[n-1]),
		AvgMS: round1(sum / float64(n)),
	}
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10.0
}

func timed(fn func() (any, error)) (float64, any, error) {
	t0 := time.Now()
	out, err := fn()
	return float64(time.Since(t0).Microseconds()) / 1000.0, out, err
}

func req(method, url string, payload any, timeout time.Duration) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func mcp(brain, method string, params any, timeout time.Duration) (map[string]any, error) {
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		payload["params"] = params
	}
	raw, err := req(http.MethodPost, strings.TrimRight(brain, "/")+"/mcp", payload, timeout)
	if err != nil {
		return nil, err
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	return d, nil
}

func mcpCall(brain, name string, arguments map[string]any, timeout time.Duration) (bool, string, error) {
	d, err := mcp(brain, "tools/call", map[string]any{"name": name, "arguments": arguments}, timeout)
	if err != nil {
		return false, "", err
	}
	res, _ := d["result"].(map[string]any)
	isErr, _ := res["isError"].(bool)
	text := ""
	if content, ok := res["content"].([]any); ok && len(content) > 0 {
		if m, ok := content[0].(map[string]any); ok {
			text, _ = m["text"].(string)
		}
	}
	return !isErr, text, nil
}

func reasonerToolCall(base, model, user string) (string, error) {
	payload := map[string]any{
		"model": model,
		"messages": []any{
			map[string]any{"role": "system", "content": "You are PicoClaw. Always call search before answering."},
			map[string]any{"role": "user", "content": user},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "deduction search",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"q": map[string]any{"type": "string"}},
						"required":   []any{"q"},
					},
				},
			},
		},
		"tool_choice": "required",
	}
	raw, err := req(http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", payload, 600*time.Second)
	if err != nil {
		return "", err
	}
	var chat struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &chat); err != nil {
		return "", err
	}
	if len(chat.Choices) == 0 || len(chat.Choices[0].Message.ToolCalls) == 0 {
		return "", nil
	}
	return chat.Choices[0].Message.ToolCalls[0].Function.Name, nil
}

type report struct {
	Brain    string          `json:"brain"`
	Device   string          `json:"device"`
	OK       bool            `json:"ok"`
	Gates    map[string]bool `json:"gates"`
	MCP      map[string]any  `json:"mcp"`
	Reasoner *reasonerBlock  `json:"reasoner,omitempty"`
	PicoClaw *picoBlock      `json:"picoclaw,omitempty"`
}

type reasonerBlock struct {
	BaseURL string         `json:"base_url"`
	Model   string         `json:"model"`
	Calls   []reasonerCall `json:"calls"`
}

type reasonerCall struct {
	MS   float64 `json:"ms"`
	Tool string  `json:"tool"`
}

type picoBlock struct {
	URL      string  `json:"url"`
	HealthMS float64 `json:"health_ms"`
	Status   string  `json:"status"`
}

func run(c cfg) (*report, error) {
	rep := &report{
		Brain:  strings.TrimRight(c.brain, "/"),
		Device: "cpu",
		OK:     true,
		Gates:  map[string]bool{},
		MCP:    map[string]any{},
	}

	ms, _, err := timed(func() (any, error) { return req(http.MethodGet, rep.Brain+"/health", nil, 5*time.Second) })
	if err != nil {
		return nil, err
	}
	rep.MCP["health"] = map[string]any{"n": 1, "avg_ms": round1(ms)}
	rep.Gates["health"] = ms <= gateHealthMS
	if ms > gateHealthMS {
		rep.OK = false
	}

	listMS := make([]float64, 0, c.n)
	for i := 0; i < c.n; i++ {
		ms, d, err := timed(func() (any, error) { return mcp(rep.Brain, "tools/list", nil, 10*time.Second) })
		if err != nil {
			return nil, err
		}
		res, _ := d.(map[string]any)["result"].(map[string]any)
		tools, _ := res["tools"].([]any)
		hasSearch := false
		for _, t := range tools {
			if tm, ok := t.(map[string]any); ok && tm["name"] == "search" {
				hasSearch = true
			}
		}
		if !hasSearch {
			rep.OK = false
		}
		listMS = append(listMS, ms)
	}
	rep.MCP["tools_list"] = stats(listMS)

	auditMS := make([]float64, 0, c.n)
	for i := 0; i < c.n; i++ {
		ms, ok, err := timed(func() (any, error) {
			ok, _, err := mcpCall(rep.Brain, "audit", map[string]any{}, 10*time.Second)
			return ok, err
		})
		if err != nil {
			return nil, err
		}
		if ok, _ := ok.(bool); !ok {
			rep.OK = false
		}
		auditMS = append(auditMS, ms)
	}
	rep.MCP["audit"] = stats(auditMS)
	rep.Gates["audit_p50"] = rep.MCP["audit"].(dist).P50MS <= gateAuditP50MS
	if !rep.Gates["audit_p50"] {
		rep.OK = false
	}

	ok, text, err := mcpCall(rep.Brain, "search", map[string]any{"q": "LadybugDB", "n": 2}, 90*time.Second)
	if err != nil {
		return nil, err
	}
	var inner struct {
		Count   int `json:"count"`
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
		Web struct {
			Status string `json:"status"`
		} `json:"web"`
	}
	if ok {
		_ = json.Unmarshal([]byte(text), &inner)
	}
	leafID := ""
	if len(inner.Results) > 0 {
		leafID = inner.Results[0].ID
	}
	rep.MCP["search_seed"] = map[string]any{
		"ok":    ok,
		"count": inner.Count,
		"web":   inner.Web.Status,
	}

	if leafID != "" {
		getMS := make([]float64, 0, c.n)
		for i := 0; i < c.n; i++ {
			ms, out, err := timed(func() (any, error) {
				ok, _, err := mcpCall(rep.Brain, "get", map[string]any{"id": leafID, "body": true}, 10*time.Second)
				return ok, err
			})
			if err != nil {
				return nil, err
			}
			if ok, _ := out.(bool); !ok {
				rep.OK = false
			}
			getMS = append(getMS, ms)
		}
		rep.MCP["get"] = stats(getMS)
		rep.Gates["get_p50"] = rep.MCP["get"].(dist).P50MS <= gateGetP50MS
		if !rep.Gates["get_p50"] {
			rep.OK = false
		}

		conc := make([]float64, 8)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				ms, _, _ := timed(func() (any, error) {
					ok, _, err := mcpCall(rep.Brain, "get", map[string]any{"id": leafID, "body": true}, 10*time.Second)
					return ok, err
				})
				conc[i] = ms
			}(i)
		}
		t0 := time.Now()
		wg.Wait()
		wall := float64(time.Since(t0).Microseconds()) / 1000.0
		rep.MCP["get_concurrent_8"] = map[string]any{"dist": stats(conc), "wall_ms": round1(wall)}
	}

	searchMS := make([]float64, 0, 2)
	for _, q := range []string{"LadybugDB", "model2vec"} {
		ms, out, err := timed(func() (any, error) {
			ok, text, err := mcpCall(rep.Brain, "search", map[string]any{"q": q, "n": 3}, 90*time.Second)
			return [2]any{ok, text}, err
		})
		if err != nil {
			return nil, err
		}
		pair := out.([2]any)
		ok, text := pair[0].(bool), pair[1].(string)
		var inner struct {
			Count int `json:"count"`
			Web   struct {
				Status string `json:"status"`
			} `json:"web"`
		}
		if ok {
			_ = json.Unmarshal([]byte(text), &inner)
		}
		searchMS = append(searchMS, ms)
		samples, _ := rep.MCP["search_samples"].([]any)
		rep.MCP["search_samples"] = append(samples, map[string]any{
			"q": q, "ms": round1(ms), "ok": ok, "count": inner.Count, "web": inner.Web.Status,
		})
	}
	if len(searchMS) > 0 {
		rep.MCP["search"] = stats(searchMS)
	}

	if c.reasoner {
		rb := &reasonerBlock{BaseURL: c.reasonerURL, Model: c.model}
		for _, user := range []string{
			"Use tools. Search the 2dph brain for LadybugDB. Call search.",
			"Use tools. Search the 2dph brain for model2vec. Call search.",
		} {
			ms, name, err := timed(func() (any, error) { return reasonerToolCall(c.reasonerURL, c.model, user) })
			if err != nil {
				return nil, err
			}
			rb.Calls = append(rb.Calls, reasonerCall{MS: round1(ms), Tool: name.(string)})
		}
		rep.Reasoner = rb
		allSearch := len(rb.Calls) > 0
		for _, call := range rb.Calls {
			if call.Tool != "search" {
				allSearch = false
			}
		}
		rep.Gates["reasoner_tool_call"] = allSearch
		if !allSearch {
			rep.OK = false
		}
	}

	if c.picoclaw {
		gw := strings.TrimRight(c.picoclawURL, "/")
		ms, raw, err := timed(func() (any, error) { return req(http.MethodGet, gw+"/health", nil, 5*time.Second) })
		if err != nil {
			return nil, err
		}
		var body struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(raw.([]byte), &body)
		rep.PicoClaw = &picoBlock{URL: gw, HealthMS: round1(ms), Status: body.Status}
		rep.Gates["picoclaw_health"] = body.Status == "ok" && ms <= gateHealthMS
		if !rep.Gates["picoclaw_health"] {
			rep.OK = false
		}
	}
	return rep, nil
}

type cfg struct {
	brain, reasonerURL, model, picoclawURL string
	n                                      int
	reasoner, picoclaw, jsonOut            bool
}

func parseFlags(args []string) (cfg, error) {
	c := cfg{brain: defaultBrain, reasonerURL: defaultReasoner, model: defaultModel, picoclawURL: defaultPicoClaw, n: 20}
	if v := os.Getenv("BRAIN_URL"); v != "" {
		c.brain = v
	}
	if v := os.Getenv("REASONER_BASE_URL"); v != "" {
		c.reasonerURL = v
	}
	if v := os.Getenv("REASONER_MODEL"); v != "" {
		c.model = v
	}
	if v := os.Getenv("PICOCLAW_URL"); v != "" {
		c.picoclawURL = v
	}
	p := cliparse.New("system_perf")
	p.Description = "2dph system performance (MCP + optional reasoner)"
	p.String(&c.brain, "", "brain", "brain base URL")
	p.Int(&c.n, "", "n", "sample count per gate")
	p.Bool(&c.jsonOut, "", "json", "print JSON report")
	p.Bool(&c.reasoner, "", "reasoner", "measure reasoner tool calls")
	p.Bool(&c.picoclaw, "", "picoclaw", "check PicoClaw gateway health")
	p.String(&c.picoclawURL, "", "picoclaw-url", "PicoClaw gateway URL")
	p.String(&c.reasonerURL, "", "reasoner-url", "OpenAI-compatible reasoner base URL")
	p.String(&c.model, "", "model", "reasoner model id")
	if err := cliparse.Parse(p, args); err != nil {
		return c, err
	}
	return c, nil
}

func main() {
	c, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "system_perf:", err)
		os.Exit(1)
	}
	rep, err := run(c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "system_perf:", err)
		os.Exit(1)
	}
	if c.jsonOut {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("ok=%v brain=%s\n", rep.OK, rep.Brain)
		for name, block := range rep.MCP {
			if d, ok := block.(dist); ok {
				fmt.Printf("  %s: p50=%.1f p95=%.1f n=%d\n", name, d.P50MS, d.P95MS, d.N)
			} else if name == "health" {
				if m, ok := block.(map[string]any); ok {
					fmt.Printf("  health: %v ms\n", m["avg_ms"])
				}
			}
		}
		for k, v := range rep.Gates {
			fmt.Printf("  gate %s: %v\n", k, v)
		}
		if rep.Reasoner != nil {
			for _, call := range rep.Reasoner.Calls {
				fmt.Printf("  reasoner %s: %.1f ms\n", call.Tool, call.MS)
			}
		}
	}
	if !rep.OK {
		os.Exit(1)
	}
}
