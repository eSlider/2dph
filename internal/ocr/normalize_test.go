package ocr

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeRawImagePDF builds a synthetic PDF whose content stream holds an
// UNCOMPRESSED 1200x1200 RGB image plus a base-14 Helvetica text run. It is the
// offline stand-in for an export-protected / oversized PDF: pdftotext cannot
// read the original (syntax errors), but Ghostscript normalizes it to a small
// PDF from which pdftotext extracts the text. Generated at test time so no
// large binary fixture is committed (AGENTS: no >few-hundred-KB fixtures).
func writeRawImagePDF(t *testing.T, path string) {
	t.Helper()
	const w, h = 1200, 1200
	raw := make([]byte, 0, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x > 400 && x < 800 && y > 500 && y < 700 {
				raw = append(raw, 20, 20, 20)
			} else {
				raw = append(raw, 245, 245, 245)
			}
		}
	}
	content := "q 1200 0 0 1200 0 0 cm /Im1 Do Q\n" +
		"BT /F1 24 Tf 72 72 Td (INVOICE-7C2F9E) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 3 0 R >> /XObject << /Im1 5 0 R >> >> /Contents 6 0 R >>",
		"<< /Type /XObject /Subtype /Image /Width 1200 /Height 1200 " +
			"/ColorSpace /DeviceRGB /BitsPerComponent 8 >> stream\n" + string(raw) + "\nendstream",
		"<< /Length " + itoa(len(content)) + " >> stream\n" + content + "\nendstream",
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
	b.WriteString("startxref\n" + itoa(xref) + "\n%%EOF\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

func zeroPad(n int) string {
	s := itoa(n)
	for len(s) < 10 {
		s = "0" + s
	}
	return s
}

func requirePDFTools(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"gs", "pdftotext"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
}

// TestNormalizePDFShrinksAndUnlocks is the #102 acceptance: gs normalizes a
// pdf-handler input, the working artifact is no larger than the original, and
// the extracted text comes out via the pdftotext fast path.
func TestNormalizePDFShrinksAndUnlocks(t *testing.T) {
	requirePDFTools(t)
	work := t.TempDir()
	t.Setenv("2DPH_VAR_TMP", work)

	orig := filepath.Join(work, "invoice.pdf")
	writeRawImagePDF(t, orig)

	origSize := fileSize(t, orig)
	if origSize == 0 {
		t.Fatal("fixture generated empty pdf")
	}
	// A "sizeable" original: uncompressed image stream several MB. This stands
	// in for the export-protected/oversized case (a true owner-password-locked
	// fixture is not crafted offline); gs normalization is what unlocks/shrinks
	// it so the pdftotext fast path can read it.
	if origSize < 1<<20 {
		t.Fatalf("fixture not sizeable: %d bytes", origSize)
	}

	norm, err := NormalizePDF(orig)
	if err != nil {
		t.Fatal(err)
	}
	if norm == orig {
		t.Fatal("NormalizePDF returned the original path; expected a gs artifact")
	}
	t.Cleanup(func() { os.Remove(norm) })

	normSize := fileSize(t, norm)
	if normSize == 0 {
		t.Fatalf("gs artifact empty: %s", norm)
	}
	if normSize > origSize {
		t.Fatalf("artifact %d bytes > original %d bytes", normSize, origSize)
	}

	text, err := pdfToText(norm)
	if err != nil {
		t.Fatalf("pdftotext on normalized artifact failed: %v", err)
	}
	if !strings.Contains(strings.ToUpper(text), "INVOICE-7C2F9E") {
		t.Fatalf("text layer not extracted from normalized pdf: %q", text)
	}
}

// TestNormalizePDFPreservesOriginal guards the evidence-first rule: gs reads
// the original, it never overwrites it.
func TestNormalizePDFPreservesOriginal(t *testing.T) {
	requirePDFTools(t)
	work := t.TempDir()
	t.Setenv("2DPH_VAR_TMP", work)

	orig := filepath.Join(work, "invoice.pdf")
	writeRawImagePDF(t, orig)
	before := fileSize(t, orig)

	norm, err := NormalizePDF(orig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(norm) })

	if after := fileSize(t, orig); after != before {
		t.Fatalf("original modified by normalization: %d → %d", before, after)
	}
}

// TestNormalizePDFWithoutGSKeepsOriginal ensures the pipeline still works when
// ghostscript is absent: NormalizePDF degrades to the original path.
func TestNormalizePDFWithoutGSKeepsOriginal(t *testing.T) {
	if _, err := exec.LookPath("gs"); err != nil {
		t.Skip("gs not installed")
	}
	// Hide gs from LookPath by prepending an empty dir to PATH.
	t.Setenv("PATH", t.TempDir())
	orig := filepath.Join(t.TempDir(), "x.pdf")
	if err := os.WriteFile(orig, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := NormalizePDF(orig)
	if err != nil {
		t.Fatal(err)
	}
	if got != orig {
		t.Fatalf("without gs expected original path, got %s", got)
	}
}

// TestPDFFileCleanFastPath guards the happy path: a PDF with a readable text
// layer goes through pdftotext directly and gs is NOT invoked (no var/tmp
// artifact is produced), so clean PDFs stay fast.
func TestPDFFileCleanFastPathSkipsGS(t *testing.T) {
	requirePDFTools(t)
	work := t.TempDir()
	t.Setenv("2DPH_VAR_TMP", work)

	clean := filepath.Join(work, "clean.pdf")
	if err := os.WriteFile(clean, []byte("%PDF-1.4 not really used: pdftotext needs valid pdf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = clean
	// Build a genuinely clean one-page text PDF (no image, no compression need).
	buildCleanTextPDF(t, clean)

	text, err := PDFFile(clean)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(text), "HELLO") {
		t.Fatalf("clean pdf text not extracted: %q", text)
	}
	entries, err := os.ReadDir(work)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "gs-") {
			t.Fatalf("gs ran on a clean pdf: %s", e.Name())
		}
	}
}

func buildCleanTextPDF(t *testing.T, path string) {
	t.Helper()
	content := "BT /F1 24 Tf 72 720 Td (HELLO-WORLD) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [4 0 R] /Count 1 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 3 0 R >> >> /Contents 5 0 R >>",
		"<< /Length " + itoa(len(content)) + " >> stream\n" + content + "\nendstream",
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
	b.WriteString("startxref\n" + itoa(xref) + "\n%%EOF\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}
