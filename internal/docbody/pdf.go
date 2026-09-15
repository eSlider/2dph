package docbody

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/docgraph"
	"github.com/eSlider/2dph/internal/docpipe"
	"github.com/eSlider/2dph/internal/ocr"
)

// PDFExtractor extracts text from PDF bytes. It reuses the hybrid docpipe
// handler (pdftotext -layout fast path, liteparse for scans when reachable) and
// falls back to internal/ocr (tesseract) when docpipe yields nothing.
//
// Opts is passed through to docpipe.Handle; tests inject Opts.PDFToText to
// keep extraction offline (no liteparse/docker call).
type PDFExtractor struct {
	Opts docpipe.Opts
}

// Extract writes data to a temp PDF and runs the docpipe → ocr chain.
func (e PDFExtractor) Extract(ctx context.Context, r docgraph.Row, data []byte) (string, error) {
	path, cleanup, err := writeTempPDF(data, r.Filename)
	if err != nil {
		return "", err
	}
	defer cleanup()

	res, derr := docpipe.Handle(ctx, path, e.Opts)
	if derr == nil && strings.TrimSpace(res.Text) != "" {
		return res.Text, nil
	}
	text, oerr := ocr.PDFFile(path)
	if oerr != nil {
		if derr != nil {
			return "", fmt.Errorf("docbody: docpipe: %v; ocr: %w", derr, oerr)
		}
		return "", fmt.Errorf("docbody: ocr: %w", oerr)
	}
	return text, nil
}

// writeTempPDF materializes data as a temp .pdf file, returning the path and a
// cleanup func. The filename hint is only used for the suffix (PDF parsers key
// on content, not name).
func writeTempPDF(data []byte, hint string) (string, func(), error) {
	ext := ".pdf"
	if h := strings.ToLower(filepath.Ext(strings.TrimSpace(hint))); h != "" {
		ext = h
	}
	f, err := os.CreateTemp("", "2dph-docbody-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("docbody: temp: %w", err)
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		cleanup()
		return "", nil, fmt.Errorf("docbody: temp write: %w", err)
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("docbody: temp close: %w", err)
	}
	return f.Name(), cleanup, nil
}
