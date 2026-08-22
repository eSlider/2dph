// Package ocr runs Tesseract (eng+deu) on images and scanned PDFs.
//
// Default engine is the tesseract CLI, not gosseract CGO: Ladybug CGO stays
// Zig-only (D21). Same engine, no gocv. OCR_ENGINE=paddle selects paddleocr
// when that binary is on PATH (compose profile ocr-paddle).
//
// PDFs go through the pdf-handler registry (internal/mailconv): pdftotext fast
// path first; when there is no readable text layer (export-locked / oversized /
// scanned), Ghostscript normalizes the PDF before extraction (NormalizePDF),
// then the pdftotext fast path / tesseract fallback run on the normalized
// artifact. The original is preserved; gs output is a transient var/tmp file.
package ocr

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const TessLang = "eng+deu"

func ImageFile(path string) (string, error) {
	engine := os.Getenv("OCR_ENGINE")
	if engine == "paddle" {
		return runPaddle(path)
	}
	return runTesseract(path)
}

func PDFFile(path string) (string, error) {
	text, err := pdfToText(path)
	if err == nil && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), nil
	}
	// Clean PDF has no text layer (scanned) or pdftotext failed (export-locked /
	// oversized): normalize with Ghostscript BEFORE extraction. The original is
	// preserved; gs output is a transient working artifact that we remove.
	norm := path
	if n, nerr := NormalizePDF(path); nerr == nil {
		norm = n
		if n != path {
			defer os.Remove(n)
		}
		text, err = pdfToText(norm)
		if err == nil && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text), nil
		}
	}
	ocr, oerr := pdfPages(norm)
	if oerr != nil {
		if err != nil {
			return "", err
		}
		return "", oerr
	}
	if strings.TrimSpace(ocr) != "" {
		return strings.TrimSpace(ocr), nil
	}
	if text != "" {
		return strings.TrimSpace(text), nil
	}
	return "", fmt.Errorf("pdf has no text layer (ocr unavailable)")
}

func pdfToText(path string) (string, error) {
	cmd := exec.Command("pdftotext", "-layout", path, "-")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func pdfPages(path string) (string, error) {
	dir, err := os.MkdirTemp("", "2dph-ocr-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	prefix := filepath.Join(dir, "page")
	cmd := exec.Command("pdftoppm", "-png", "-r", "200", path, prefix)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	matches, err := filepath.Glob(prefix + "*.png")
	if err != nil {
		return "", err
	}
	var parts []string
	for _, img := range matches {
		t, err := ImageFile(img)
		if err != nil {
			continue
		}
		if s := strings.TrimSpace(t); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n"), nil
}

func runTesseract(path string) (string, error) {
	pre, err := preprocessFile(path)
	if err != nil {
		pre = path
	} else {
		defer os.Remove(pre)
	}
	cmd := exec.Command("tesseract", pre, "stdout", "-l", lang(), "--psm", "6")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// lang returns TessLang unless OCR_LANG overrides it (CI containers may ship
// fewer language packs, e.g. jitesoft/tesseract-ocr has eng only).
func lang() string {
	if v := os.Getenv("OCR_LANG"); v != "" {
		return v
	}
	return TessLang
}

func runPaddle(path string) (string, error) {
	cmd := exec.Command("paddleocr", "ocr", "-i", path)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func preprocessFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return "", err
	}
	out := filepath.Join(os.TempDir(), filepath.Base(path)+".gray.png")
	w, err := os.Create(out)
	if err != nil {
		return "", err
	}
	defer w.Close()
	if err := png.Encode(w, GrayContrast(img)); err != nil {
		os.Remove(out)
		return "", err
	}
	return out, nil
}

// GrayContrast is a stdlib preprocess (no gocv): grayscale + stretch.
func GrayContrast(src image.Image) image.Image {
	b := src.Bounds()
	dst := image.NewGray(b)
	var minL, maxL uint8 = 255, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := color.GrayModel.Convert(src.At(x, y)).(color.Gray)
			if g.Y < minL {
				minL = g.Y
			}
			if g.Y > maxL {
				maxL = g.Y
			}
		}
	}
	span := int(maxL) - int(minL)
	if span < 1 {
		span = 1
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := color.GrayModel.Convert(src.At(x, y)).(color.Gray)
			v := uint8((int(g.Y) - int(minL)) * 255 / span)
			dst.SetGray(x, y, color.Gray{Y: v})
		}
	}
	return dst
}
