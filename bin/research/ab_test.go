//go:build research_ab

// bin/research/ab_test.go — offline tests for the A/B research tool
// (issue #221). No network: docker is wrapped via an injectable runner and
// skipped when unavailable; baseline runs against a synthetic tiny PDF.
package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/ocr"
	"gopkg.in/yaml.v3"
)

// fixtureJSON is a synthetic liteparse --format json --extract-blocks output
// (page text_items + blocks), used by converter and metrics tests.
const fixtureJSON = `{
  "total_pages": 1,
  "pages": [
    {
      "page": 1,
      "width": 612.0,
      "height": 792.0,
      "text": "INVOICE-7C2F9E\n\nClient: Acme GmbH",
      "text_items": [
        {"text": "INVOICE-7C2F9E", "x": 72.0, "y": 56.88, "width": 129.8, "height": 18.7, "font_name": "Helvetica", "font_size": 16.0, "confidence": 1.0},
        {"text": "Client: Acme GmbH", "x": 72.0, "y": 91.6, "width": 97.1, "height": 12.8, "font_name": "Helvetica", "font_size": 11.0, "confidence": 0.98}
      ],
      "blocks": [
        {"kind": "heading", "text": "INVOICE-7C2F9E", "level": 1, "bbox": {"x": 72.0, "y": 56.88, "width": 129.8, "height": 18.7}},
        {"kind": "paragraph", "text": "Client: Acme GmbH", "bbox": {"x": 72.0, "y": 91.6, "width": 97.1, "height": 12.8}},
        {"kind": "table", "text": "Item Qty", "bbox": {"x": 72.0, "y": 121.6, "width": 128.0, "height": 8.4}}
      ]
    }
  ]
}`

func TestJSONToYAMLConvertsBlocks(t *testing.T) {
	y, err := JSONToYAML([]byte(fixtureJSON))
	if err != nil {
		t.Fatalf("JSONToYAML: %v", err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(y, &m); err != nil {
		t.Fatalf("yaml unmarshal: %v\n%s", err, y)
	}
	if got := num(m["total_pages"]); got != 1 {
		t.Errorf("total_pages = %v, want 1", m["total_pages"])
	}
	pages, ok := m["pages"].([]any)
	if !ok || len(pages) != 1 {
		t.Fatalf("pages = %#v, want 1 entry", m["pages"])
	}
	page := pages[0].(map[string]any)
	blocks := page["blocks"].([]any)
	if len(blocks) != 3 {
		t.Fatalf("blocks len = %d, want 3", len(blocks))
	}
	h := blocks[0].(map[string]any)
	if h["kind"] != "heading" || num(h["level"]) != 1 {
		t.Errorf("heading block = %#v, want kind=heading level=1", h)
	}
	if blocks[2].(map[string]any)["kind"] != "table" {
		t.Errorf("block[2] kind = %#v, want table", blocks[2])
	}
	items := page["text_items"].([]any)
	it := items[0].(map[string]any)
	if it["font_name"] != "Helvetica" || num(it["confidence"]) != 1 {
		t.Errorf("text_item = %#v, want Helvetica confidence 1", it)
	}
	if !strings.Contains(string(y), "kind: heading") {
		t.Errorf("yaml missing kind: heading\n%s", y)
	}
}

// num coerces int/float64 YAML scalars to float64 for assertions.
func num(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

func TestJSONToYAMLRejectsInvalid(t *testing.T) {
	if _, err := JSONToYAML([]byte("{not json")); err == nil {
		t.Fatal("JSONToYAML accepted invalid JSON")
	}
}

func TestBlockKindsAndBBoxes(t *testing.T) {
	var l LitJSON
	if err := json.Unmarshal([]byte(fixtureJSON), &l); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	kinds := l.blockKinds()
	if kinds["heading"] != 1 || kinds["paragraph"] != 1 || kinds["table"] != 1 {
		t.Errorf("blockKinds = %#v, want heading=1 paragraph=1 table=1", kinds)
	}
	if n := l.bboxCount(); n != 2 {
		t.Errorf("bboxCount = %d, want 2 text_items", n)
	}
}

func TestValidateMetrics(t *testing.T) {
	m := DocMetrics{
		File:       "invoice-text.pdf",
		Complexity: ComplexityInfo{NeedsOCR: true, Reasons: []string{"sparse-text"}},
		Baseline:   MethodResult{OK: true, MS: 12, Bytes: 90, TextLen: 45, Method: "pdftotext-layout"},
		LitMD:      MethodResult{OK: true, MS: 500, Bytes: 300},
		LitJSON:    JSONResult{OK: true, MS: 1700, Bytes: 4000, Blocks: map[string]int{"heading": 1}, BBoxes: 2},
	}
	if err := ValidateMetrics(m); err != nil {
		t.Errorf("valid metrics rejected: %v", err)
	}
	bad := m
	bad.Baseline.MS = -1
	if err := ValidateMetrics(bad); err == nil {
		t.Error("negative baseline ms accepted")
	}
	bad = m
	bad.LitJSON.Blocks["table"] = -3
	if err := ValidateMetrics(bad); err == nil {
		t.Error("negative block count accepted")
	}
}

func TestDiscoverPDFs(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"b.pdf", "a.pdf", "note.txt", "c.PDF"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := discoverPDFs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || filepath.Base(got[0]) != "a.pdf" || filepath.Base(got[1]) != "b.pdf" {
		t.Errorf("discoverPDFs = %v, want sorted a.pdf b.pdf", got)
	}
}

func TestBaselineTinyPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	pdf := tinyTextPDF(t, "INVOICE-7C2F9E")
	text, err := ocr.PDFFile(pdf)
	if err != nil {
		t.Fatalf("ocr.PDFFile: %v", err)
	}
	if !strings.Contains(text, "INVOICE-7C2F9E") {
		t.Errorf("baseline text = %q, want INVOICE-7C2F9E", text)
	}
	start := timeNow()
	_, _ = ocr.PDFFile(pdf)
	if ms := msSince(start); ms < 0 {
		t.Errorf("baseline ms = %d, want >= 0", ms)
	}
}

func TestComplexityParse(t *testing.T) {
	// stdout of `lit is-complex` is a JSON array of per-page verdicts.
	out := `[
  {"page_number": 1, "text_length": 11, "text_coverage": 0.006, "has_substantial_images": false, "image_block_count": 0, "image_coverage": 0.0, "largest_image_coverage": 0.0, "full_page_image": true, "uncovered_vector_area": null, "is_garbled": false, "page_area": 484704.0, "needs_ocr": true, "reasons": ["scanned"], "layout": {"column_count": 1, "ruled_table_count": 0, "ruled_table_coverage": 0.0, "text_table_run_count": 0, "figure_count": 0, "figure_coverage": 0.0, "is_complex": false, "reasons": []}}
]`
	c, err := parseComplexity([]byte(out))
	if err != nil {
		t.Fatalf("parseComplexity: %v", err)
	}
	if !c.NeedsOCR || len(c.Reasons) != 1 || c.Reasons[0] != "scanned" || c.IsComplex {
		t.Errorf("complexity = %+v, want needs_ocr=true reasons=[scanned] is_complex=false", c)
	}
}

// TestRunOneDocOffline proves the tool orchestration runs end-to-end with a
// fake docker runner: metrics are collected, validated and written to disk
// without any network or real liteparse.
func TestRunOneDocOffline(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	outDir := t.TempDir()
	pdf := tinyTextPDF(t, "INVOICE-7C2F9E")
	base := filepath.Base(pdf)

	fake := fakeRunner(t, outDir)
	ctx := context.Background()
	m, err := runOneDoc(ctx, fake, outDir, pdf, outDir)
	if err != nil {
		t.Fatalf("runOneDoc: %v", err)
	}
	if err := ValidateMetrics(m); err != nil {
		t.Errorf("metrics invalid: %v", err)
	}
	if !m.Complexity.NeedsOCR || m.LitMDOCTR == nil || !m.LitMDOCTR.OK {
		t.Errorf("OCR variant expected for needs_ocr doc: %+v", m)
	}
	if m.LitJSON.Blocks["heading"] != 1 || m.LitJSON.Blocks["table"] != 1 {
		t.Errorf("json blocks = %#v, want heading=1 table=1", m.LitJSON.Blocks)
	}
	for _, f := range []string{stem(base) + ".md", stem(base) + ".ocr.md", stem(base) + ".json", stem(base) + ".yaml"} {
		if _, err := os.Stat(filepath.Join(outDir, f)); err != nil {
			t.Errorf("expected output %s: %v", f, err)
		}
	}
}

// fakeRunner mimics the docker `lit ...` calls: it parses the requested
// output name from the args and writes fixture output into outDir, exactly
// like the docker mount /o -> outDir would.
func fakeRunner(t *testing.T, outDir string) runner {
	t.Helper()
	return func(_ context.Context, args ...string) ([]byte, error) {
		switch args[0] {
		case "is-complex":
			// return the fixture verdict with reason "sparse-text"
			return []byte(`[{"page_number": 1, "needs_ocr": true, "reasons": ["sparse-text"], "layout": {"is_complex": false}}]`), nil
		case "parse":
			var format string
			for i, a := range args {
				if a == "--format" && i+1 < len(args) {
					format = args[i+1]
				}
				if a == "-o" && i+1 < len(args) {
					name := strings.TrimPrefix(args[i+1], "/o/")
					switch format {
					case "markdown":
						md := "# INVOICE-7C2F9E\n\nClient: Acme GmbH\n"
						if err := os.WriteFile(filepath.Join(outDir, name), []byte(md), 0o644); err != nil {
							return nil, err
						}
					case "json":
						if err := os.WriteFile(filepath.Join(outDir, name), []byte(fixtureJSON), 0o644); err != nil {
							return nil, err
						}
					}
					return []byte("[liteparse] wrote " + name), nil
				}
			}
			return nil, os.ErrInvalid
		}
		return nil, os.ErrInvalid
	}
}

// TestLitParseDockerEndToEnd runs the real docker path on a synthetic PDF.
// Skipped when docker or the liteparse image is unavailable — no network is
// touched (local image only).
func TestLitParseDockerEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	const image = "ghcr.io/run-llama/liteparse:latest"
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		t.Skipf("liteparse image %s not pulled: %v", image, err)
	}
	dir := t.TempDir()
	pdf := filepath.Join(dir, "tiny.pdf")
	writeTinyPDF(t, pdf, "INVOICE-7C2F9E")
	r, err := dockerRunner(image, defaultPDFiumPath, dir, dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	base := filepath.Base(pdf)
	if _, err := r(ctx, "parse", "/s/"+base,
		"--format", "markdown", "--extract-blocks", "--no-ocr", "-o", "/o/"+stem(base)+".md"); err != nil {
		t.Fatalf("lit parse: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, stem(base)+".md"))
	if err != nil {
		t.Fatalf("read md: %v", err)
	}
	if !strings.Contains(string(b), "INVOICE") {
		t.Errorf("md = %q, want INVOICE", b)
	}
}

// tinyTextPDF writes a minimal single-page PDF with a Helvetica text run.
func tinyTextPDF(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tiny.pdf")
	writeTinyPDF(t, path, text)
	return path
}

func writeTinyPDF(t *testing.T, path, text string) {
	t.Helper()
	content := "BT /F1 16 Tf 72 720 Td (" + text + ") Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Length " + itoa(len(content)) + " >> stream\n" + content + "endstream",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		b.WriteString(itoa(i+1) + " 0 obj\n" + o + "\nendobj\n")
	}
	xref := b.Len()
	b.WriteString("xref\n0 " + itoa(len(objs)+1) + "\n0000000000 65535 f \n")
	for _, off := range offsets {
		b.WriteString(zeroPad(off) + " 00000 n \n")
	}
	b.WriteString("trailer\n<< /Size " + itoa(len(objs)+1) + " /Root 1 0 R >>\n")
	b.WriteString("startxref\n" + itoa(xref) + "\n%%EOF")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}
