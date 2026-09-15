package docbody

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/docgraph"
	"github.com/eSlider/2dph/internal/docpipe"
)

// TestPDFExtractorStub proves the docpipe wiring: bytes are materialized as a
// temp PDF and the fast-path extractor's text is returned, no docker/network.
func TestPDFExtractorStub(t *testing.T) {
	e := PDFExtractor{Opts: docpipe.Opts{
		PDFToText: func(ctx context.Context, path string) (string, error) {
			if filepath.Ext(path) != ".pdf" {
				t.Errorf("temp file ext = %q, want .pdf", filepath.Ext(path))
			}
			return "  STUB BODY 4711  ", nil
		},
	}}
	got, err := e.Extract(context.Background(), docgraph.Row{Filename: "r.pdf"}, []byte("%PDF-1.4"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "STUB BODY 4711" {
		t.Fatalf("text = %q", got)
	}
}

// TestPDFExtractorRealPDF runs the real pdftotext fast path on a generated
// PDF fixture. Skipped when pdftotext is absent (offline stub covers the rest).
func TestPDFExtractorRealPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	path := filepath.Join(t.TempDir(), "fixture.pdf")
	writeMinimalPDF(t, path, "Rechnung Nr 4711 Betrag 123,45 EUR")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := (PDFExtractor{}).Extract(context.Background(), docgraph.Row{Filename: "fixture.pdf"}, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "4711") || !strings.Contains(got, "123,45") {
		t.Fatalf("pdftotext text = %q", got)
	}
}

// writeMinimalPDF writes a one-page text-layer PDF with a correct xref table.
func writeMinimalPDF(t *testing.T, path, text string) {
	t.Helper()
	content := "BT /F1 24 Tf 72 700 Td (" + escapePDFString(text) + ") Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", len(objs)+1)
	b.WriteString("0000000000 65535 f \n")
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func escapePDFString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "(", `\(`)
	return strings.ReplaceAll(s, ")", `\)`)
}
