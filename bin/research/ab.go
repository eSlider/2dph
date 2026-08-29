//usr/bin/env go run -tags=research_ab "$0" "$@"; exit
//go:build research_ab
//
// bin/research/ab.go — A/B: internal/ocr.PDFFile baseline vs liteparse
// (markdown + JSON→YAML) on var/research/samples (#221).
//
//	./bin/research/ab.go                       # default samples+out dirs
//	./bin/research/ab.go --samples <dir> --out <dir>
//	LITEPARSE_IMAGE=... ./bin/research/ab.go   # override docker image
//	LITEPARSE_PDFIUM_LIB_PATH=... ./bin/research/ab.go
//
// For each samples/*.pdf it runs: `lit is-complex` verdict, the baseline
// (pdftotext fast path, gs/tesseract fallback), `lit parse --format
// markdown --extract-blocks --no-ocr` (+ OCR variant when the verdict asks),
// and `lit parse --format json --extract-blocks` converted to YAML. Metrics
// land in var/research/out/metrics.json; artifacts in var/research/out/.
// liteparse runs as an external docker command (image layout requires
// PDFIUM_LIB_PATH/LD_LIBRARY_PATH env and /s, /o mounts).
// NOTE: never run gofmt -w on this file — it breaks the shebang.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eSlider/2dph/internal/ocr"
	"gopkg.in/yaml.v3"
)

const (
	defaultSamples    = "var/research/samples"
	defaultOut        = "var/research/out"
	defaultImage      = "ghcr.io/run-llama/liteparse:latest"
	defaultPDFiumPath = "/usr/local/lib/pdfium-rs/chromium_7897/pdfium-linux-x64/lib"
	litTimeout        = 10 * time.Minute
)

// ---------------------------------------------------------------------------
// Metrics types

// DocMetrics aggregates one sample document across baseline (internal/ocr),
// liteparse markdown (no-ocr + optional OCR variant) and liteparse JSON.
type DocMetrics struct {
	File       string         `json:"file"`
	Error      string         `json:"error,omitempty"` // first per-doc step failure
	Complexity ComplexityInfo `json:"complexity"`
	Baseline   MethodResult   `json:"baseline"`
	LitMD      MethodResult   `json:"litparse_md"`
	LitMDOCTR  *MethodResult  `json:"litparse_md_ocr,omitempty"` // only when verdict needs_ocr
	LitJSON    JSONResult     `json:"litparse_json"`
}

// ComplexityInfo is the aggregated `lit is-complex` verdict: any page with
// needs_ocr=true marks the document; reasons/layouts are unioned.
type ComplexityInfo struct {
	NeedsOCR  bool     `json:"needs_ocr"`
	Reasons   []string `json:"reasons"`
	IsComplex bool     `json:"is_complex"`
}

// MethodResult is wall-time + output size of one extraction method.
type MethodResult struct {
	OK      bool   `json:"ok"`
	MS      int64  `json:"ms"`
	Bytes   int    `json:"bytes"`
	TextLen int    `json:"text_len,omitempty"`
	Method  string `json:"method,omitempty"` // baseline fast path: pdftotext-layout | ocr-fallback
	Error   string `json:"error,omitempty"`
}

// JSONResult carries the structured --format json --extract-blocks metrics.
type JSONResult struct {
	OK     bool           `json:"ok"`
	MS     int64          `json:"ms"`
	Bytes  int            `json:"bytes"`
	Blocks map[string]int `json:"blocks_by_kind"`
	BBoxes int            `json:"bboxes"` // text_items with positional data
	Error  string         `json:"error,omitempty"`
}

// ValidateMetrics rejects impossible numbers (negative ms/bytes/counts).
func ValidateMetrics(m DocMetrics) error {
	check := func(what string, v int64) error {
		if v < 0 {
			return fmt.Errorf("%s: negative value %d", what, v)
		}
		return nil
	}
	for what, v := range map[string]int64{
		"baseline.ms":          m.Baseline.MS,
		"baseline.bytes":       int64(m.Baseline.Bytes),
		"baseline.text_len":    int64(m.Baseline.TextLen),
		"litparse_md.ms":       m.LitMD.MS,
		"litparse_md.bytes":    int64(m.LitMD.Bytes),
		"litparse_json.ms":     m.LitJSON.MS,
		"litparse_json.bytes":  int64(m.LitJSON.Bytes),
		"litparse_json.bboxes": int64(m.LitJSON.BBoxes),
	} {
		if err := check(what, v); err != nil {
			return err
		}
	}
	if m.LitMDOCTR != nil {
		if err := check("litparse_md_ocr.ms", m.LitMDOCTR.MS); err != nil {
			return err
		}
		if err := check("litparse_md_ocr.bytes", int64(m.LitMDOCTR.Bytes)); err != nil {
			return err
		}
	}
	for k, n := range m.LitJSON.Blocks {
		if n < 0 {
			return fmt.Errorf("litparse_json.blocks[%s]: negative count %d", k, n)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// liteparse JSON -> YAML conversion

// LitJSON mirrors the liteparse `lit parse --format json` envelope.
type LitJSON struct {
	TotalPages int       `json:"total_pages" yaml:"total_pages"`
	Pages      []LitPage `json:"pages" yaml:"pages"`
}

// LitPage is one page: full-text dump, positional text_items and, with
// --extract-blocks, semantic blocks.
type LitPage struct {
	Page      int        `json:"page" yaml:"page"`
	Width     float64    `json:"width" yaml:"width"`
	Height    float64    `json:"height" yaml:"height"`
	Text      string     `json:"text" yaml:"text"`
	TextItems []LitItem  `json:"text_items" yaml:"text_items"`
	Blocks    []LitBlock `json:"blocks" yaml:"blocks"`
}

// LitItem is a positioned text fragment (bbox = x/y/width/height + font).
type LitItem struct {
	Text       string  `json:"text" yaml:"text"`
	X          float64 `json:"x" yaml:"x"`
	Y          float64 `json:"y" yaml:"y"`
	Width      float64 `json:"width" yaml:"width"`
	Height     float64 `json:"height" yaml:"height"`
	FontName   string  `json:"font_name" yaml:"font_name"`
	FontSize   float64 `json:"font_size" yaml:"font_size"`
	Confidence float64 `json:"confidence" yaml:"confidence"`
}

// LitBBox is the positional box of a block. The current liteparse image
// emits an object {x, y, width, height} (older builds used a 4-element
// array).
type LitBBox struct {
	X      float64 `json:"x" yaml:"x"`
	Y      float64 `json:"y" yaml:"y"`
	Width  float64 `json:"width" yaml:"width"`
	Height float64 `json:"height" yaml:"height"`
}

// LitBlock is one semantic block: heading/paragraph/table/list/...
type LitBlock struct {
	Kind  string   `json:"kind" yaml:"kind"`
	Text  string   `json:"text" yaml:"text"`
	Level *int     `json:"level,omitempty" yaml:"level,omitempty"` // heading level
	BBox  *LitBBox `json:"bbox,omitempty" yaml:"bbox,omitempty"`
}

// JSONToYAML decodes liteparse JSON and re-encodes it as YAML.
func JSONToYAML(src []byte) ([]byte, error) {
	var l LitJSON
	if err := json.Unmarshal(src, &l); err != nil {
		return nil, fmt.Errorf("liteparse json: %w", err)
	}
	out, err := yaml.Marshal(&l)
	if err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	return out, nil
}

// blockKinds counts blocks per kind across all pages.
func (l LitJSON) blockKinds() map[string]int {
	m := map[string]int{}
	for _, p := range l.Pages {
		for _, b := range p.Blocks {
			m[b.Kind]++
		}
	}
	return m
}

// bboxCount counts positioned text_items (structural evidence: geometry).
func (l LitJSON) bboxCount() int {
	n := 0
	for _, p := range l.Pages {
		n += len(p.TextItems)
	}
	return n
}

// ---------------------------------------------------------------------------
// docker runner + per-document orchestration

// runner abstracts `docker run ... lit <args>`: args exclude the leading
// docker invocation; returns stdout.
type runner func(ctx context.Context, args ...string) ([]byte, error)

// dockerRunner binds a runner to the real docker CLI. samplesAbs/outAbs are
// mounted read-only / read-write at /s and /o inside the container.
func dockerRunner(image, pdfiumLibPath, samplesAbs, outAbs string) (runner, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found on PATH: %w", err)
	}
	return func(ctx context.Context, args ...string) ([]byte, error) {
		full := []string{"run", "--rm",
			"-e", "PDFIUM_LIB_PATH=" + pdfiumLibPath,
			"-e", "LD_LIBRARY_PATH=" + pdfiumLibPath,
			"-v", samplesAbs + ":/s:ro",
			"-v", outAbs + ":/o",
			image, "lit",
		}
		full = append(full, args...)
		cctx, cancel := context.WithTimeout(ctx, litTimeout)
		defer cancel()
		cmd := exec.CommandContext(cctx, "docker", full...)
		out, err := cmd.Output()
		if err != nil && len(out) == 0 {
			msg := err.Error()
			if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
				msg = strings.TrimSpace(string(ee.Stderr))
			}
			return nil, fmt.Errorf("lit %v: %w (%s)", args, err, msg)
		}
		// lit is-complex exits 1 when a page needs OCR, yet still prints the
		// verdict JSON to stdout — keep the output, the caller decides.
		return out, nil
	}, nil
}

// complexityPage is one per-page `lit is-complex` verdict.
type complexityPage struct {
	PageNumber int      `json:"page_number"`
	NeedsOCR   bool     `json:"needs_ocr"`
	Reasons    []string `json:"reasons"`
	Layout     struct {
		IsComplex bool     `json:"is_complex"`
		Reasons   []string `json:"reasons"`
	} `json:"layout"`
}

// parseComplexity aggregates the is-complex JSON array (stdout).
func parseComplexity(out []byte) (ComplexityInfo, error) {
	var pages []complexityPage
	if err := json.Unmarshal(out, &pages); err != nil {
		return ComplexityInfo{}, fmt.Errorf("is-complex json: %w", err)
	}
	var c ComplexityInfo
	seen := map[string]struct{}{}
	for _, p := range pages {
		c.NeedsOCR = c.NeedsOCR || p.NeedsOCR
		c.IsComplex = c.IsComplex || p.Layout.IsComplex
		for _, r := range append(append([]string{}, p.Reasons...), p.Layout.Reasons...) {
			if _, ok := seen[r]; !ok {
				seen[r] = struct{}{}
				c.Reasons = append(c.Reasons, r)
			}
		}
	}
	sort.Strings(c.Reasons)
	return c, nil
}

// runOneDoc measures one sample across baseline + liteparse and writes the
// markdown/json/yaml artifacts into outDir. pdfPath is the host path;
// samplesDir is the host dir mounted at /s.
func runOneDoc(ctx context.Context, r runner, outDir, pdfPath, samplesDir string) (DocMetrics, error) {
	base := filepath.Base(pdfPath)
	name := stem(base)
	m := DocMetrics{File: base}
	sampleErr := func(step string, err error) {
		m.Error = fmt.Sprintf("%s: %v", step, err)
	}

	// 1. complexity verdict (stdout = JSON array; exit 1 = needs OCR).
	if out, rerr := r(ctx, "is-complex", "/s/"+base); rerr != nil && len(out) == 0 {
		sampleErr("is-complex", rerr)
	} else if c, perr := parseComplexity(out); perr != nil {
		sampleErr("is-complex", perr)
	} else {
		m.Complexity = c
	}

	// 2. baseline: internal/ocr.PDFFile (pdftotext fast path, gs/tesseract
	// fallback) — wall time is the honest pipeline cost.
	m.Baseline = collectBaseline(pdfPath)

	// 3. liteparse markdown, text-layer fast path.
	if ms, err := runLit(r, ctx, "parse", "/s/"+base,
		"--format", "markdown", "--extract-blocks", "--no-ocr", "-o", "/o/"+name+".md"); err != nil {
		m.LitMD = MethodResult{OK: false, Error: err.Error()}
	} else {
		m.LitMD = MethodResult{OK: true, MS: ms, Bytes: fileBytes(filepath.Join(outDir, name+".md"))}
	}

	// 4. OCR variant only when the verdict demands it (scanned/sparse docs).
	if m.Complexity.NeedsOCR {
		if ms, err := runLit(r, ctx, "parse", "/s/"+base,
			"--format", "markdown", "--extract-blocks", "-o", "/o/"+name+".ocr.md"); err != nil {
			m.LitMDOCTR = &MethodResult{OK: false, Error: err.Error()}
		} else {
			m.LitMDOCTR = &MethodResult{OK: true, MS: ms, Bytes: fileBytes(filepath.Join(outDir, name+".ocr.md"))}
		}
	}

	// 5. structured JSON (default OCR behavior) -> YAML artifact.
	jsonPath := filepath.Join(outDir, name+".json")
	if ms, err := runLit(r, ctx, "parse", "/s/"+base,
		"--format", "json", "--extract-blocks", "-o", "/o/"+name+".json"); err != nil {
		m.LitJSON = JSONResult{OK: false, Error: err.Error()}
	} else if raw, err := os.ReadFile(jsonPath); err != nil {
		m.LitJSON = JSONResult{OK: false, Error: err.Error()}
	} else if y, err := JSONToYAML(raw); err != nil {
		m.LitJSON = JSONResult{OK: false, Error: err.Error()}
	} else if err := os.WriteFile(filepath.Join(outDir, name+".yaml"), y, 0o644); err != nil {
		m.LitJSON = JSONResult{OK: false, Error: err.Error()}
	} else {
		var l LitJSON
		_ = json.Unmarshal(raw, &l) // raw already validated by JSONToYAML
		m.LitJSON = JSONResult{
			OK:     true,
			MS:     ms,
			Bytes:  len(raw),
			Blocks: l.blockKinds(),
			BBoxes: l.bboxCount(),
		}
	}
	return m, nil
}

// runLit executes one docker lit call and returns wall-time ms.
func runLit(r runner, ctx context.Context, args ...string) (int64, error) {
	start := timeNow()
	if _, err := r(ctx, args...); err != nil {
		return msSince(start), err
	}
	return msSince(start), nil
}

// collectBaseline times internal/ocr.PDFFile and classifies the fast path.
func collectBaseline(path string) MethodResult {
	start := timeNow()
	text, err := ocr.PDFFile(path)
	res := MethodResult{
		OK:      err == nil,
		MS:      msSince(start),
		Bytes:   len(text),
		TextLen: len(strings.TrimSpace(text)),
		Method:  "pdftotext-layout",
	}
	if probe, perr := exec.Command("pdftotext", "-layout", path, "-").Output(); perr != nil || strings.TrimSpace(string(probe)) == "" {
		res.Method = "ocr-fallback" // gs normalize + pdftotext, or pdftoppm + tesseract
	}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

// ---------------------------------------------------------------------------
// helpers

// discoverPDFs returns *.pdf files in dir, sorted for deterministic reports.
func discoverPDFs(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.pdf"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

func stem(name string) string { return strings.TrimSuffix(name, ".pdf") }

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func zeroPad(n int) string {
	s := itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}

func timeNow() time.Time { return time.Now() }

func msSince(start time.Time) int64 { return time.Since(start).Milliseconds() }

func fileBytes(path string) int {
	if st, err := os.Stat(path); err == nil {
		return int(st.Size())
	}
	return 0
}

// ---------------------------------------------------------------------------
// CLI

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("ab", flag.ContinueOnError)
	samples := fs.String("samples", defaultSamples, "directory with sample PDFs")
	out := fs.String("out", defaultOut, "output directory for metrics.json and artifacts")
	image := fs.String("image", envOr("LITEPARSE_IMAGE", defaultImage), "liteparse docker image")
	pdfium := fs.String("pdfium-lib-path", envOr("LITEPARSE_PDFIUM_LIB_PATH", defaultPDFiumPath), "PDFIUM_LIB_PATH inside the image")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: ab.go [--samples dir] [--out dir] [--image img] [--pdfium-lib-path p]\n")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	samplesAbs, err := filepath.Abs(*samples)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ab: samples dir: %v\n", err)
		return 1
	}
	outAbs, err := filepath.Abs(*out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ab: out dir: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(outAbs, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "ab: mkdir out: %v\n", err)
		return 1
	}

	docs, err := discoverPDFs(samplesAbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ab: discover samples: %v\n", err)
		return 1
	}
	if len(docs) == 0 {
		fmt.Fprintf(os.Stderr, "ab: no *.pdf in %s (run gen-samples.go first)\n", samplesAbs)
		return 1
	}

	r, err := dockerRunner(*image, *pdfium, samplesAbs, outAbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ab: %v\n", err)
		return 1
	}

	ctx := context.Background()
	var report []DocMetrics
	for _, pdf := range docs {
		fmt.Fprintf(os.Stderr, "ab: %s ...\n", filepath.Base(pdf))
		m, err := runOneDoc(ctx, r, outAbs, pdf, samplesAbs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ab: %s: %v\n", filepath.Base(pdf), err)
			return 1
		}
		if err := ValidateMetrics(m); err != nil {
			fmt.Fprintf(os.Stderr, "ab: %s: invalid metrics: %v\n", filepath.Base(pdf), err)
			return 1
		}
		report = append(report, m)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "ab: metrics json: %v\n", err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(outAbs, "metrics.json"), append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "ab: write metrics: %v\n", err)
		return 1
	}
	printTable(report)
	fmt.Printf("\nmetrics -> %s\n", filepath.Join(*out, "metrics.json"))
	return 0
}

// printTable renders the report as a compact stdout table.
func printTable(report []DocMetrics) {
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "document\tverdict\tbaseline ms\tbaseline method\tmd ms\tmd bytes\tmd_ocr ms\tjson ms\tjson bytes\tblocks\tbboxes")
	for _, m := range report {
		ks := make([]string, 0, len(m.LitJSON.Blocks))
		for k := range m.LitJSON.Blocks {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		blocks := make([]string, 0, len(ks))
		for _, k := range ks {
			blocks = append(blocks, fmt.Sprintf("%s=%d", k, m.LitJSON.Blocks[k]))
		}
		verdict := "text"
		if m.Complexity.NeedsOCR {
			verdict = "needs-ocr(" + strings.Join(m.Complexity.Reasons, ",") + ")"
		}
		if m.Complexity.IsComplex {
			verdict += "/complex"
		}
		mdOCR := "-"
		if m.LitMDOCTR != nil {
			mdOCR = fmt.Sprintf("%d", m.LitMDOCTR.MS)
		} else if m.Complexity.NeedsOCR {
			mdOCR = "err"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\t%d\t%s\t%d\t%d\t%s\t%d\n",
			m.File,
			verdict,
			m.Baseline.MS, m.Baseline.Method,
			m.LitMD.MS, m.LitMD.Bytes,
			mdOCR,
			m.LitJSON.MS, m.LitJSON.Bytes,
			strings.Join(blocks, ","),
			m.LitJSON.BBoxes,
		)
	}
	_ = w.Flush()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
