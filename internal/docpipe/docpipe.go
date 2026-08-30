// Package docpipe implements the hybrid PDF handler PoC (epic #219, issue
// #223): a pdftotext -layout fast path for text-layer PDFs plus the warm
// liteparse JSON path (docker compose exec via internal/research.Runner) for
// scans/complex documents, with table reconstruction from text_items
// geometry (y-buckets → rows, x-sort → cells).
//
// The output table tree follows the #100 shape (KindTable/Row/Cell with
// canonical content URLs: docpipe://<platform>/<thread>/<msg>/page[i]/
// table[j]/tr[k]/td[m]). JSON→YAML reuses internal/research (single
// implementation — LitJSON.MarshalYAML), never a local converter.
//
// Docker independence: the liteparse call goes through the LitParser
// interface (satisfied by *research.Runner) and the fast path through the
// PDFToText func — offline tests stub both; the docker path is skipped when
// the service is unreachable.
package docpipe

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/address"
	"github.com/eSlider/2dph/internal/research"
)

// Method identifies which extraction path produced a Result.
type Method int

const (
	// MethodFastPath is the pdftotext -layout path (text-layer PDFs).
	MethodFastPath Method = iota
	// MethodLiteparse is the warm liteparse JSON path (scans/complex docs).
	MethodLiteparse
)

func (m Method) String() string {
	switch m {
	case MethodFastPath:
		return "fastpath"
	case MethodLiteparse:
		return "liteparse"
	default:
		return "unknown"
	}
}

// URLBase carries the #100 content-addressing prefix for reconstructed
// nodes. Empty fields select defaults; Handle fills Thread with the source
// sha256[:16] when unset.
type URLBase struct {
	Scheme   string
	Platform string
	Thread   string
	Msg      string
}

// normalized returns the base with defaults applied (Scheme/Platform/Msg).
func (b URLBase) normalized() URLBase {
	if b.Scheme == "" {
		b.Scheme = "docpipe"
	}
	if b.Platform == "" {
		b.Platform = "pdf"
	}
	if b.Msg == "" {
		b.Msg = "doc"
	}
	return b
}

// Cell is one reconstructed table cell: text + geometry provenance + URL.
type Cell struct {
	Text string
	URL  string
	X    float64
	Y    float64
	W    float64
	H    float64
}

// Row is one y-bucket row: cells sorted by x (left to right).
type Row struct {
	URL   string
	Y     float64 // bucket anchor y (top of the visual line)
	Cells []Cell
}

// Table is a reconstructed table: a maximal run of multi-cell rows.
type Table struct {
	URL  string
	Page int
	Rows []Row
}

// LitParser is the warm liteparse call hook. *research.Runner satisfies it;
// offline tests substitute a fixture-backed stub (docker path skipped).
type LitParser interface {
	ParseToJSON(ctx context.Context, inPath string, o research.ParseOpts) (*research.LitJSON, error)
}

// PDFToText extracts a PDF's text layer. Default: pdftotext -layout via exec
// (same engine as internal/ocr.PDFFile).
type PDFToText func(ctx context.Context, path string) (string, error)

// Opts controls one hybrid run.
type Opts struct {
	LitParser LitParser
	// ForceLiteparse routes to the warm service even when the fast path
	// yields text (scans whose pdftotext output is a meaningless stub, or
	// callers that need structure/geometry, e.g. table reconstruction).
	ForceLiteparse bool
	// OCR enables tesseract on the liteparse path (scans).
	OCR bool
	// Base is the #100 addressing prefix for reconstructed tables.
	Base URLBase
	// PDFToText overrides the fast path extractor (tests).
	PDFToText PDFToText
}

// Result is the hybrid handler output.
type Result struct {
	Method      Method
	Text        string
	JSON        *research.LitJSON // non-nil on the liteparse path
	YAML        []byte            // research.LitJSON.MarshalYAML on the liteparse path
	Tables      []Table           // reconstructed from text_items (liteparse path)
	FastPathMs  float64           // 0 when the fast path was not attempted
	LiteparseMs float64           // 0 when the service was not called
}

// Handle runs the hybrid pipeline on one PDF: fast path first (non-empty text
// wins, method=fastpath); on empty text, error, or ForceLiteparse the warm
// liteparse service is called (method=liteparse) and tables are reconstructed
// from text_items geometry.
func Handle(ctx context.Context, path string, o Opts) (*Result, error) {
	lp := o.LitParser
	if lp == nil {
		lp = research.NewRunner("liteparse", 3*time.Minute)
	}
	pdfToText := o.PDFToText
	if pdfToText == nil {
		pdfToText = defaultPDFToText
	}

	res := &Result{}
	var fastErr error
	if !o.ForceLiteparse {
		t0 := time.Now()
		text, err := pdfToText(ctx, path)
		res.FastPathMs = msSince(t0)
		if err == nil && strings.TrimSpace(text) != "" {
			res.Method = MethodFastPath
			res.Text = strings.TrimSpace(text)
			return res, nil
		}
		fastErr = err
	}

	t0 := time.Now()
	l, err := lp.ParseToJSON(ctx, containerPath(path),
		research.ParseOpts{Format: "json", OCR: o.OCR, Blocks: true})
	res.LiteparseMs = msSince(t0)
	if err != nil {
		if fastErr != nil {
			return nil, fmt.Errorf("docpipe: fast path: %v; liteparse: %w", fastErr, err)
		}
		return nil, fmt.Errorf("docpipe: liteparse: %w", err)
	}

	res.Method = MethodLiteparse
	res.JSON = l
	res.Text = strings.TrimSpace(l.RawText())
	if y, yerr := l.MarshalYAML(); yerr != nil {
		return nil, fmt.Errorf("docpipe: yaml: %w", yerr)
	} else {
		res.YAML = y
	}

	base := o.Base.normalized()
	if base.Thread == "" {
		if h, herr := research.FileHash(path); herr == nil && len(h) >= 16 {
			base.Thread = h[:16]
		} else {
			base.Thread = "doc"
		}
	}
	res.Tables = ReconstructTables(l.Pages, base)
	return res, nil
}

// defaultPDFToText extracts the text layer with pdftotext -layout (same
// engine as internal/ocr.PDFFile; duplicated because ocr's helper is
// unexported and context-less).
func defaultPDFToText(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", path, "-")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// containerPath maps a host path to the liteparse container path: the host
// var/ tree mounts at /v (see compose.yaml), so var/research/samples/x.pdf →
// /v/research/samples/x.pdf.
func containerPath(path string) string {
	rel, err := filepath.Rel(filepath.Dir(research.StructDir), path)
	if err != nil {
		return "/v/" + filepath.ToSlash(path)
	}
	return "/v/" + filepath.ToSlash(rel)
}

// ReconstructTables rebuilds tables from text_items geometry: items are
// grouped into rows by y-buckets (threshold ≈ half the median font size),
// cells within a row are sorted by x, and a table is a maximal run of
// consecutive multi-cell rows (≥ 2 rows). Single-cell lines (titles, notes)
// never join a table. Nodes get #100-style URLs under base.
//
// When liteparse reports kind:table blocks with a bbox (bordered tables —
// real invoices/contracts), each table is reconstructed from the text_items
// falling inside its bbox only: this separates adjacent tables and keeps
// wrapped header/cell lines from leaking into neighbouring rows. Fallback:
// the whole page, split on multi-cell runs.
func ReconstructTables(pages []research.LitPage, base URLBase) []Table {
	base = base.normalized()
	var tables []Table
	for pi, p := range pages {
		if len(p.TextItems) == 0 {
			continue
		}
		bboxTables := pageBBoxTables(p)
		if len(bboxTables) > 0 {
			for bi, bt := range bboxTables {
				items := itemsInBBox(p.TextItems, bt)
				rows := bucketRows(items, yThreshold(items))
				rows = mergeWrappedRows(rows)
				tab := rowsToTable(base, pi, bi, rows)
				if tab != nil {
					tables = append(tables, *tab)
				}
			}
			continue
		}
		rows := bucketRows(p.TextItems, yThreshold(p.TextItems))
		tableIdx := 0
		var cur []Row
		flush := func() {
			if len(cur) < 2 {
				cur = nil
				return
			}
			tab := rowsToTable(base, pi, tableIdx, cur)
			if tab != nil {
				tables = append(tables, *tab)
			}
			tableIdx++
			cur = nil
		}
		for _, r := range rows {
			if len(r.Cells) >= 2 {
				cur = append(cur, r)
				continue
			}
			flush()
		}
		flush()
	}
	return tables
}

// pageBBoxTables returns kind:table blocks that carry a bbox (bordered
// tables detected by liteparse's layout classifier).
func pageBBoxTables(p research.LitPage) []research.LitBlock {
	var out []research.LitBlock
	for _, b := range p.Blocks {
		if b.Kind == "table" && b.BBox != nil {
			out = append(out, b)
		}
	}
	return out
}

// itemsInBBox filters text_items whose top-left corner falls inside the
// table block's bounding box.
func itemsInBBox(items []research.LitItem, b research.LitBlock) []research.LitItem {
	out := make([]research.LitItem, 0, len(items))
	if b.BBox == nil {
		return items
	}
	bb := b.BBox
	for _, it := range items {
		if it.X >= bb.X && it.X < bb.X+bb.Width &&
			it.Y >= bb.Y && it.Y < bb.Y+bb.Height {
			out = append(out, it)
		}
	}
	return out
}

// mergeWrappedRows merges consecutive rows that are actually a wrapped cell
// line: only a row with fewer cells than the previous one whose first cell
// falls inside the previous row's first cell band gets merged into it.
// Handles LibreOffice/Pandoc line wraps inside table cells ("Consulting" /
// "services") without collapsing genuine multi-column data rows.
func mergeWrappedRows(rows []Row) []Row {
	if len(rows) == 0 {
		return rows
	}
	out := []Row{rows[0]}
	for i := 1; i < len(rows); i++ {
		prev := &out[len(out)-1]
		cur := rows[i]
		if len(cur.Cells) == 1 && len(prev.Cells) > 1 && len(prev.Cells) > 0 {
			// single-cell wrap line: merge into the column band it continues.
			c0 := cur.Cells[0]
			for ci := range prev.Cells {
				c := &prev.Cells[ci]
				if c0.X >= c.X && c0.X < c.X+c.W {
					c.Text += " " + c0.Text
					goto merged
				}
			}
		}
		out = append(out, cur)
	merged:
	}
	return out
}

// rowsToTable builds a Table from a run of rows, or nil when too short.
func rowsToTable(base URLBase, pageIdx, tableIdx int, rows []Row) *Table {
	// drop single-cell padding rows at the top (header-only "Descriptio n"
	// split) and the bottom (wrapped note) — keep the multi-cell core.
	var core []Row
	for _, r := range rows {
		if len(r.Cells) >= 2 {
			core = append(core, r)
		}
	}
	if len(core) < 2 {
		return nil
	}
	url := nodeURL(base, pageIdx, tableIdx)
	tr := make([]Row, len(core))
	for j, r := range core {
		tr[j] = r
		tr[j].URL = nodeURL(base, pageIdx, tableIdx) + "/tr[" + itoa(j) + "]"
		for m := range tr[j].Cells {
			tr[j].Cells[m].URL = tr[j].URL + "/td[" + itoa(m) + "]"
		}
	}
	return &Table{URL: url, Page: pageIdx, Rows: tr}
}

// bucketRows groups items into visual lines: items whose y differs from the
// bucket's anchor y by at most threshold share a row. Cells are sorted by x.
func bucketRows(items []research.LitItem, threshold float64) []Row {
	ordered := make([]research.LitItem, len(items))
	copy(ordered, items)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Y != ordered[j].Y {
			return ordered[i].Y < ordered[j].Y
		}
		if ordered[i].X != ordered[j].X {
			return ordered[i].X < ordered[j].X
		}
		return ordered[i].Text < ordered[j].Text
	})

	var rows []Row
	for _, it := range ordered {
		if len(rows) == 0 || it.Y-rows[len(rows)-1].Y > threshold {
			rows = append(rows, Row{Y: it.Y})
		}
		r := &rows[len(rows)-1]
		r.Cells = append(r.Cells, Cell{
			Text: it.Text, X: it.X, Y: it.Y, W: it.Width, H: it.Height,
		})
	}
	for i := range rows {
		cells := rows[i].Cells
		sort.SliceStable(cells, func(a, b int) bool { return cells[a].X < cells[b].X })
	}
	return rows
}

// yThreshold is the y-bucket tolerance: half the median font size, floored at
// 2pt so degenerate fonts still produce sane buckets.
func yThreshold(items []research.LitItem) float64 {
	var sizes []float64
	for _, it := range items {
		if it.FontSize > 0 {
			sizes = append(sizes, it.FontSize)
		}
	}
	if len(sizes) == 0 {
		return 2.0
	}
	sort.Float64s(sizes)
	med := sizes[len(sizes)/2]
	if th := med / 2; th > 2.0 {
		return th
	}
	return 2.0
}

// nodeURL builds the #100 URL for table[tableIdx] on page pageIdx: segs are
// page[i], table[j] (+ tr/td appended by the caller).
func nodeURL(base URLBase, pageIdx, tableIdx int) string {
	segs := []address.Segment{
		{Type: "page", Index: pageIdx, HasIndex: true},
		{Type: "table", Index: tableIdx, HasIndex: true},
	}
	u, err := address.New(base.Scheme, base.Platform, base.Thread, base.Msg, segs, "")
	if err != nil {
		// defaults are always grammar-valid; keep a degraded fallback
		return fmt.Sprintf("%s://%s/%s/%s/page[%d]/table[%d]",
			base.Scheme, base.Platform, base.Thread, base.Msg, pageIdx, tableIdx)
	}
	return u
}

func msSince(t0 time.Time) float64 {
	return float64(time.Since(t0).Microseconds()) / 1000.0
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
