package ocr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Ghostscript normalization for the pdf-handler (#8) BEFORE extraction.
//
// gs -sDEVICE=pdfwrite re-renders the PDF: it strips export-protection (owner
// password not enforced), re-embeds fonts as subsets and re-compresses image
// streams, so the output is typically no larger than the input and often much
// smaller (not only images). A normalized copy lets the pdftotext fast path
// (and tesseract fallback) succeed where the raw export-locked PDF was
// unreadable.
//
// The ORIGINAL input is never modified (evidence-first): it is preserved in
// the corpus; gs output is a working artifact under var/tmp (gitignored).
// If ghostscript is unavailable, NormalizePDF returns the original path so the
// pipeline degrades to the existing pdftotext/tesseract behaviour on the
// original.
//
// TODO(#102): red — stub returns the input unchanged until gs is wired in.
func NormalizePDF(path string) (string, error) {
	if _, err := exec.LookPath("gs"); err != nil {
		return path, nil
	}
	dir, err := workDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	out := filepath.Join(dir, "gs-"+base+"-0.pdf")
	cmd := exec.Command("gs",
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.4",
		"-dPDFSETTINGS=/ebook",
		"-dNOPAUSE", "-dQUIET", "-dBATCH",
		"-sOutputFile="+out,
		path,
	)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gs normalize %s: %w", path, err)
	}
	return path, nil
}

// workDir returns the var/tmp root for gs working artifacts. 2DPH_VAR_TMP
// overrides it (used by tests so no repo pollution).
func workDir() (string, error) {
	if v := os.Getenv("2DPH_VAR_TMP"); v != "" {
		return v, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(wd, "var", "tmp"), nil
}
