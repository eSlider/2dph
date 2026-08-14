//usr/bin/env go run -tags=reasoner_bakeoff "$0" "$@"; exit
//go:build reasoner_bakeoff
//
// bin/reasoner/bakeoff.go - CPU tool-call bake-off against an OpenAI-compatible URL (D18).
//
//	REASONER_BASE_URL=http://127.0.0.1:11435/v1 REASONER_MODEL=qwen3.5:9b ./bin/reasoner/bakeoff.go
//	./bin/reasoner/bakeoff.go --model MichelRosselli/bonsai-27b:Q1_0 --json
//
// Measures OpenAI tool_calls (search/get/audit) and RSS from Ollama /api/ps, not VRAM.
// PicoClaw is compose profile picoclaw; tool names match internal/httpapi MCP ops.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/eSlider/2dph/internal/duckstats"
	"github.com/eSlider/2dph/internal/reasoner"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	base := os.Getenv("REASONER_BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:11435/v1"
	}
	model := os.Getenv("REASONER_MODEL")
	if model == "" {
		model = reasoner.OllamaRAM
	}
	jsonOut := false
	device := "cpu"
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--json":
			jsonOut = true
		case a == "--model" && i+1 < len(args):
			i++
			model = args[i]
		case strings.HasPrefix(a, "--model="):
			model = strings.TrimPrefix(a, "--model=")
		case a == "--base-url" && i+1 < len(args):
			i++
			base = args[i]
		case a == "--device" && i+1 < len(args):
			i++
			device = args[i]
		case a == "-h" || a == "--help":
			fmt.Fprintln(os.Stderr, "bin/reasoner/bakeoff.go [--model ID] [--base-url URL] [--device cpu] [--json]")
			return 0
		default:
			fmt.Fprintln(os.Stderr, "unknown arg:", a)
			return 2
		}
	}
	c := reasoner.Client{BaseURL: base, Model: model, Device: device}
	rep := reasoner.Run(c)
	lat := make([]float64, 0, len(rep.Prompts))
	for _, p := range rep.Prompts {
		lat = append(lat, float64(p.LatencyMS))
	}
	if st, err := duckstats.Quantiles(lat); err == nil {
		rep.LatencyP50MS = st.P50
		rep.LatencyP95MS = st.P95
	}
	raw, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if jsonOut {
		fmt.Println(string(raw))
	} else {
		fmt.Printf("model: %s\n", rep.Model)
		fmt.Printf("hf_id: %s\n", rep.HF)
		fmt.Printf("device: %s\n", rep.Device)
		fmt.Printf("tool_call: %d/%d\n", rep.ToolCallOK, rep.ToolCallN)
		fmt.Printf("xml_leak: %d\n", rep.XMLLeak)
		fmt.Printf("rss_mb: %d\n", rep.RSSMB)
		fmt.Printf("vram_mb: %d\n", rep.VRAMMB)
		fmt.Printf("latency_p50_ms: %g\n", rep.LatencyP50MS)
		fmt.Printf("latency_p95_ms: %g\n", rep.LatencyP95MS)
		for _, p := range rep.Prompts {
			status := "fail"
			if p.OK {
				status = "ok"
			}
			fmt.Printf("  %s: %s wanted=%s got=%s xml=%v %dms %s\n", p.WantedTool, status, p.WantedTool, p.ToolName, p.XMLLeak, p.LatencyMS, p.Err)
		}
	}
	if rep.ToolCallN == 0 {
		return 1
	}
	return 0
}
