// Package docbody enriches gator kind=document Leafs with the text of the PDF
// body (T12, #310). The document tree carries only metadata + a reference
// (oo_path/sha256); the bytes live in OnlyOffice. Provider combines a Fetcher
// (artifact bytes, e.g. OnlyOffice by oo_path/ref) and an Extractor (PDF →
// text, docpipe/ocr fast path) behind docgraph.BodyProvider, so the corpus
// wiring stays cgo-free and tests substitute both halves offline.
//
// Default is metadata-only: callers enable body extraction explicitly
// (bin/mail/leaf.go --document-text / GATOR_DOC_TEXT). A fetch/extract failure
// is not fatal — the metadata leaf is kept.
package docbody

import (
	"context"
	"strings"

	"github.com/eSlider/2dph/internal/docgraph"
)

// Fetcher downloads the artifact bytes referenced by a document row.
type Fetcher interface {
	Fetch(ctx context.Context, r docgraph.Row) ([]byte, error)
}

// Extractor turns artifact bytes into plain text.
type Extractor interface {
	Extract(ctx context.Context, r docgraph.Row, data []byte) (string, error)
}

// Provider binds a Fetcher and an Extractor and implements
// docgraph.BodyProvider. Non-PDF rows yield an empty body (no fetch).
type Provider struct {
	Fetch   Fetcher
	Extract Extractor
}

// Body fetches and extracts the PDF body for r. Empty string means "no body
// available"; the caller keeps the metadata leaf. Nil halves are a no-op.
func (p Provider) Body(ctx context.Context, r docgraph.Row) (string, error) {
	if p.Fetch == nil || p.Extract == nil {
		return "", nil
	}
	if !IsPDF(r) {
		return "", nil
	}
	data, err := p.Fetch.Fetch(ctx, r)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	return p.Extract.Extract(ctx, r, data)
}

// IsPDF reports whether the row is a PDF (by MIME or filename). Rows without
// either hint are treated as non-PDF.
func IsPDF(r docgraph.Row) bool {
	if strings.Contains(strings.ToLower(r.MIME), "pdf") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(r.Filename)), ".pdf")
}
