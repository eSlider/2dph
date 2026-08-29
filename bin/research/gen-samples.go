//usr/bin/env go run -tags=research_gen "$0" "$@"; exit
//go:build research_gen
//
// bin/research/gen-samples.go — generate the isolated A/B sample set under
// var/research/samples/ (gitignored; prod corpus var/corpus is never touched).
//
//	./bin/research/gen-samples.go                 # write all samples
//	./bin/research/gen-samples.go --clean         # wipe var/research/samples
//
// Samples are synthetic, built at runtime so no large binary fixture is
// committed (AGENTS: no >few-hundred-KB fixtures):
//   - invoice-text.pdf  — real text layer: heading, table (3 rows), list.
//   - invoice-scan.pdf  — scan-like: raw RGB image page + tiny text run.
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	out := filepath.Join("var", "research", "samples")
	if len(os.Args) > 1 && os.Args[1] == "--clean" {
		_ = os.RemoveAll(out)
		fmt.Println("removed", out)
		return
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(1)
	}
	if err := writeTextPDF(filepath.Join(out, "invoice-text.pdf")); err != nil {
		fmt.Fprintln(os.Stderr, "text pdf:", err)
		os.Exit(1)
	}
	if err := writeScanPDF(filepath.Join(out, "invoice-scan.pdf")); err != nil {
		fmt.Fprintln(os.Stderr, "scan pdf:", err)
		os.Exit(1)
	}
	fmt.Println("samples written to", out)
}

// writeTextPDF builds a small text-layer PDF with a heading, a table and a
// list — the structure a good md/JSON pipeline should recover.
func writeTextPDF(path string) error {
	content := "BT /F1 16 Tf 72 720 Td (INVOICE-7C2F9E) Tj ET\n" +
		"BT /F1 11 Tf 72 690 Td (Client: Acme GmbH) Tj ET\n" +
		"BT /F1 11 Tf 72 660 Td (Item          Qty    Price) Tj ET\n" +
		"BT /F1 11 Tf 72 640 Td (Widget A       2      10.00) Tj ET\n" +
		"BT /F1 11 Tf 72 620 Td (Widget B       1      25.00) Tj ET\n" +
		"BT /F1 11 Tf 72 600 Td (Total               45.00) Tj ET\n" +
		"BT /F1 11 Tf 72 570 Td (Note: net 14 days.) Tj ET\n"
	return writePDF(path, content, "")
}

// writeScanPDF builds a scan-like PDF: a full-page raw RGB image (light
// background with a dark band) plus one text run. pdftotext fails on it, so
// OCR (tesseract / liteparse OCR) is required to read anything beyond the run.
func writeScanPDF(path string) error {
	const w, h = 900, 900
	raw := make([]byte, 0, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if y > 300 && y < 450 {
				raw = append(raw, 20, 20, 20)
			} else {
				raw = append(raw, 245, 245, 245)
			}
		}
	}
	content := "q 900 0 0 900 0 0 cm /Im1 Do Q\n" +
		"BT /F1 20 Tf 72 72 Td (SCAN-9F31A1) Tj ET\n"
	imgObj := "<< /Type /XObject /Subtype /Image /Width 900 /Height 900 " +
		"/ColorSpace /DeviceRGB /BitsPerComponent 8 >> stream\n" + string(raw) + "\nendstream"
	return writePDF(path, content, imgObj)
}

// writePDF assembles a minimal single-page PDF (objects, xref, trailer) with
// an optional image XObject and an uncompressed content stream.
func writePDF(path, content, imgObj string) error {
	var hasImg bool
	resources := "/Font << /F1 3 0 R >>"
	kids := "4 0 R"
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << " + resources + " >> /Contents 5 0 R >>"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [" + kids + "] /Count 1 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		page,
		"<< /Length " + itoa(len(content)) + " >> stream\n" + content + "\nendstream",
	}
	if imgObj != "" {
		hasImg = true
		// page gains an XObject resource; image object id = 6.
		objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << " + resources + " /XObject << /Im1 6 0 R >> >> /Contents 5 0 R >>"
		objs = append(objs, imgObj)
	}
	_ = hasImg

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
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func zeroPad(n int) string {
	s := itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}