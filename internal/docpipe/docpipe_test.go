// Offline tests for the hybrid docpipe handler (epic #219, issue #223).
// All fixtures are synthetic (invoice from the A/B study #221/#220, values
// taken from var/struct-data/<sha256(invoice-text.pdf)>.yml) and embedded
// here — no network, no docker, no model. Docker-dependent paths are stubbed
// via the LitParser/PDFToText hooks; the live run lives in live_test.go under
// the research_docpipe build tag.
package docpipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/research"
)

// invoiceTextItems reproduces the text_items of var/research/samples/
// invoice-text.pdf (from var/struct-data/<hash>.yml): title, client line,
// 4-row table (header + 2 data + total), note.
func invoiceTextItems() []research.LitItem {
	mk := func(text string, x, y, w, h, fs float64) research.LitItem {
		return research.LitItem{Text: text, X: x, Y: y, Width: w, Height: h,
			FontName: "Helvetica", FontSize: fs, Confidence: 1}
	}
	return []research.LitItem{
		mk("INVOICE-7C2F9E", 72.0, 56.88, 129.808, 18.70398, 16),
		mk("Client: Acme GmbH", 72.0, 91.60498, 97.18501, 12.859009, 11),
		// header row
		mk("Item", 72.0, 121.60498, 21.395004, 12.859009, 11),
		mk("Qty", 123.97499, 121.60498, 17.115997, 12.859009, 11),
		mk("Price", 153.32298, 121.60498, 25.057999, 12.859009, 11),
		// data row 1
		mk("Widget A", 72.0, 141.60498, 44.627, 12.859009, 11),
		mk("2", 138.03299, 141.60498, 6.1159973, 12.859009, 11),
		mk("10.00", 162.49698, 141.60498, 27.521988, 12.859009, 11),
		// data row 2
		mk("Widget B", 72.0, 161.60498, 44.627, 12.859009, 11),
		mk("1", 138.03299, 161.60498, 6.1159973, 12.859009, 11),
		mk("25.00", 162.49698, 161.60498, 27.521988, 12.859009, 11),
		// total row
		mk("Total", 72.0, 181.60498, 24.453003, 12.859009, 11),
		mk("45.00", 142.323, 181.60498, 27.521973, 12.859009, 11),
		// trailing single-cell line (must NOT join the table)
		mk("Note: net 14 days.", 72.0, 211.60498, 89.276, 12.859009, 11),
	}
}

func invoicePage() research.LitPage {
	return research.LitPage{
		Page: 1, Width: 612, Height: 792, TextItems: invoiceTextItems(),
		Text: "INVOICE-7C2F9E\n\nClient: Acme GmbH\n\nItem    Qty  Price\n" +
			"Widget A  2       10.00\nWidget B  1       25.00\nTotal     45.00\n\n" +
			"Note: net 14 days.",
	}
}

// rowTexts flattens a table row to its cell texts.
func rowTexts(r Row) []string {
	out := make([]string, len(r.Cells))
	for i, c := range r.Cells {
		out[i] = c.Text
	}
	return out
}

func TestReconstructInvoiceTextTable(t *testing.T) {
	tables := ReconstructTables([]research.LitPage{invoicePage()}, URLBase{})
	if len(tables) != 1 {
		t.Fatalf("ReconstructTables: got %d tables, want 1", len(tables))
	}
	tab := tables[0]
	if len(tab.Rows) != 4 {
		t.Fatalf("table rows = %d, want 4 (header + 2 data + total)", len(tab.Rows))
	}
	want := [][]string{
		{"Item", "Qty", "Price"},
		{"Widget A", "2", "10.00"},
		{"Widget B", "1", "25.00"},
		{"Total", "45.00"},
	}
	for i, w := range want {
		if got := rowTexts(tab.Rows[i]); !slicesEqual(got, w) {
			t.Errorf("row %d = %v, want %v", i, got, w)
		}
	}
	// single-cell lines (title, client, note) must not be table rows
	for _, r := range tab.Rows {
		for _, c := range r.Cells {
			for _, banned := range []string{"INVOICE", "Client:", "Note:"} {
				if strings.Contains(c.Text, banned) {
					t.Errorf("table cell %q leaked into the table (single-cell line)", c.Text)
				}
			}
		}
	}
}

func TestTableURLsFollowAddressing(t *testing.T) {
	tables := ReconstructTables([]research.LitPage{invoicePage()}, URLBase{Thread: "t1"})
	if len(tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(tables))
	}
	tab := tables[0]
	if !strings.Contains(tab.URL, "/page[0]/table[0]") {
		t.Errorf("table URL = %q, want page[0]/table[0] segment", tab.URL)
	}
	if !strings.HasPrefix(tab.URL, "docpipe://pdf/t1/doc/") {
		t.Errorf("table URL = %q, want docpipe://pdf/t1/doc/ prefix", tab.URL)
	}
	if !strings.Contains(tab.Rows[1].URL, "/tr[1]") {
		t.Errorf("row URL = %q, want /tr[1]", tab.Rows[1].URL)
	}
	if !strings.Contains(tab.Rows[1].Cells[2].URL, "/tr[1]/td[2]") {
		t.Errorf("cell URL = %q, want /tr[1]/td[2]", tab.Rows[1].Cells[2].URL)
	}
}

func TestYBucketGrouping(t *testing.T) {
	rows := bucketRows(invoiceTextItems(), 5.5)
	// 7 distinct visual lines: title, client, header, data1, data2, total, note
	if len(rows) != 7 {
		t.Fatalf("buckets = %d, want 7", len(rows))
	}
	got := make([][]string, len(rows))
	for i, r := range rows {
		got[i] = rowTexts(r)
	}
	want := [][]string{
		{"INVOICE-7C2F9E"},
		{"Client: Acme GmbH"},
		{"Item", "Qty", "Price"},
		{"Widget A", "2", "10.00"},
		{"Widget B", "1", "25.00"},
		{"Total", "45.00"},
		{"Note: net 14 days."},
	}
	for i := range want {
		if !slicesEqual(got[i], want[i]) {
			t.Errorf("bucket %d = %v, want %v", i, got[i], want[i])
		}
	}
	// same-y items share a bucket; 20pt-apart lines fall into different buckets
	if gap := rows[3].Y - rows[2].Y; gap <= 5.5 {
		t.Errorf("bucket gap = %v, want > 5.5 (distinct visual lines)", gap)
	}
}

func TestNoTableWhenNoMultiCellRows(t *testing.T) {
	p := research.LitPage{Page: 1, TextItems: []research.LitItem{
		{Text: "one", X: 10, Y: 10, FontSize: 11},
		{Text: "two", X: 10, Y: 30, FontSize: 11},
	}}
	if tables := ReconstructTables([]research.LitPage{p}, URLBase{}); len(tables) != 0 {
		t.Fatalf("got %d tables, want 0 (single-cell rows only)", len(tables))
	}
}

// --- hybrid handler ---------------------------------------------------------

// stubLitParser is the offline hook replacing the warm liteparse service.
type stubLitParser struct {
	l   *research.LitJSON
	err error
}

func (s *stubLitParser) ParseToJSON(context.Context, string, research.ParseOpts) (*research.LitJSON, error) {
	return s.l, s.err
}

func stubLiteparseResult() *research.LitJSON {
	return &research.LitJSON{TotalPages: 1, Pages: []research.LitPage{invoicePage()}}
}

func TestHandleFastPath(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not on PATH")
	}
	dir := t.TempDir()
	pdf := filepath.Join(dir, "text.pdf")
	if err := os.WriteFile(pdf, makeTextPDF("Invoice-1", "Widget A  2  10.00"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the liteparse hook must NOT be reached on the fast path
	lp := &stubLitParser{l: stubLiteparseResult()}
	res, err := Handle(context.Background(), pdf, Opts{LitParser: lp})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Method != MethodFastPath {
		t.Fatalf("method = %s, want fastpath", res.Method)
	}
	if !strings.Contains(res.Text, "Widget A") {
		t.Errorf("text = %q, want Widget A", res.Text)
	}
	if res.LiteparseMs != 0 {
		t.Errorf("LiteparseMs = %v, want 0 (not called)", res.LiteparseMs)
	}
}

func TestHandleFallbackToLiteparse(t *testing.T) {
	empty := func(context.Context, string) (string, error) { return "", nil }
	res, err := Handle(context.Background(), "x.pdf", Opts{
		LitParser: &stubLitParser{l: stubLiteparseResult()},
		PDFToText: empty,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Method != MethodLiteparse {
		t.Fatalf("method = %s, want liteparse", res.Method)
	}
	if len(res.Tables) != 1 || len(res.Tables[0].Rows) != 4 {
		t.Errorf("tables = %d rows, want 1 table / 4 rows", len(res.Tables))
	}
	if !strings.Contains(res.Text, "Widget A") {
		t.Errorf("text = %q, want Widget A (page text)", res.Text)
	}
	if len(res.YAML) == 0 || !bytes.Contains(res.YAML, []byte("total_pages:")) {
		t.Errorf("YAML empty or missing envelope (len=%d)", len(res.YAML))
	}
	if res.FastPathMs < 0 {
		t.Errorf("FastPathMs = %v", res.FastPathMs)
	}
}

func TestHandleForceLiteparse(t *testing.T) {
	// non-empty fast path, but ForceLiteparse routes to the service anyway
	full := func(context.Context, string) (string, error) { return "plenty of text", nil }
	res, err := Handle(context.Background(), "x.pdf", Opts{
		LitParser:      &stubLitParser{l: stubLiteparseResult()},
		PDFToText:      full,
		ForceLiteparse: true,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res.Method != MethodLiteparse {
		t.Fatalf("method = %s, want liteparse", res.Method)
	}
	if res.FastPathMs != 0 {
		t.Errorf("FastPathMs = %v, want 0 (fast path not attempted)", res.FastPathMs)
	}
}

func TestHandleBothPathsFail(t *testing.T) {
	boom := func(context.Context, string) (string, error) { return "", errors.New("pdftotext boom") }
	lp := &stubLitParser{err: errors.New("service down")}
	_, err := Handle(context.Background(), "x.pdf", Opts{LitParser: lp, PDFToText: boom})
	if err == nil {
		t.Fatal("Handle succeeded, want error")
	}
	if !strings.Contains(err.Error(), "pdftotext boom") || !strings.Contains(err.Error(), "service down") {
		t.Errorf("error = %q, want both failures reported", err)
	}
}

// --- helpers ----------------------------------------------------------------

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// makeTextPDF builds a minimal single-page text-layer PDF (correct xref) so
// the fast path can run against a real file without committed fixtures.
func makeTextPDF(lines ...string) []byte {
	var stream bytes.Buffer
	y := 720.0
	for _, ln := range lines {
		fmt.Fprintf(&stream, "BT /F1 12 Tf 72 %.1f Td (%s) Tj ET\n", y, escPDFText(ln))
		y -= 20
	}
	content := stream.String()
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, obj := range objects {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objects)+1)
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= len(objects); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

func escPDFText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	return r.Replace(s)
}

// slicesContain reports whether ss contains want (exact).
func slicesContain(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// billPage reproduces the text_items of a real bordered invoice
// (var/research/samples/invoice-bill.pdf): two kind:table blocks with bbox
// (Items + Totals) and wrapped cell lines ("Consulting"/"services") that must
// merge back into one cell. Proves bbox-scoped reconstruction + wrap merge
// without docker.
func billPage() research.LitPage {
	mk := func(text string, x, y, w, h, fs float64) research.LitItem {
		return research.LitItem{Text: text, X: x, Y: y, Width: w, Height: h,
			FontName: "Helvetica", FontSize: fs, Confidence: 1}
	}
	billBox := research.LitBBox{X: 72.1, Y: 255.3, Width: 366.8, Height: 106.3}
	totalsBox := research.LitBBox{X: 72.0, Y: 394.85, Width: 252.1, Height: 64.5}
	return research.LitPage{
		Page: 1, Width: 612, Height: 792,
		Blocks: []research.LitBlock{
			{Kind: "table", BBox: &billBox},
			{Kind: "table", BBox: &totalsBox},
		},
		TextItems: []research.LitItem{
			mk("INVOICE No. INV-2026-0042", 72.1, 143.93, 200, 18, 16),
			// Items table (within billBox bbox)
			mk("n", 151.3, 255.74, 8, 12, 12),
			mk("#", 72.1, 255.58, 6, 12, 12),
			mk("Qty", 230.5, 255.30, 18, 12, 12),
			mk("Unit Price", 309.7, 255.37, 60, 12, 12),
			mk("Amount", 388.8, 255.45, 45, 12, 12),
			mk("1", 72.1, 273.60, 6, 12, 12),
			mk("Consulting", 151.3, 273.38, 70, 12, 12),
			mk("services", 151.3, 287.67, 45, 12, 12),
			mk("10", 230.5, 273.60, 12, 12, 12),
			mk("120.00", 309.7, 273.60, 35, 12, 12),
			mk("1200.00", 388.9, 273.60, 45, 12, 12),
			mk("2", 72.1, 305.20, 6, 12, 12),
			mk("Software", 151.3, 304.98, 55, 12, 12),
			mk("license", 151.3, 318.98, 40, 12, 12),
			mk("2", 230.5, 305.20, 12, 12, 12),
			mk("450.00", 309.7, 305.20, 35, 12, 12),
			mk("900.00", 388.9, 305.20, 45, 12, 12),
			mk("3", 72.1, 336.80, 6, 12, 12),
			mk("Support", 151.3, 336.80, 50, 12, 12),
			mk("(annual)", 151.3, 350.58, 50, 12, 12),
			mk("1", 230.5, 336.80, 12, 12, 12),
			mk("300.00", 309.7, 336.80, 35, 12, 12),
			mk("300.00", 388.9, 336.80, 45, 12, 12),
			// Totals table (within totalsBox bbox)
			mk("Item", 72.1, 394.85, 60, 12, 12),
			mk("Amount", 270.0, 394.85, 45, 12, 12),
			mk("Subtotal", 72.1, 412.88, 55, 12, 12),
			mk("2400.00", 270.1, 413.10, 45, 12, 12),
			mk("VAT (19%)", 72.0, 430.48, 60, 12, 12),
			mk("456.00", 270.1, 430.70, 45, 12, 12),
			mk("Total Due", 72.1, 448.18, 55, 12, 12),
			mk("2856.00", 270.1, 448.40, 45, 12, 12),
			// note (single cell, outside both bboxes)
			mk("Payment terms: net 14 days.", 72.1, 472.98, 220, 12, 12),
		},
	}
}

// TestReconstructBillTablesBBox proves bbox-scoped reconstruction of two
// bordered tables plus wrap-merge of split cell lines — the regression for
// the bill sample (#223 follow-up).
func TestReconstructBillTablesBBox(t *testing.T) {
	tables := ReconstructTables([]research.LitPage{billPage()}, URLBase{})
	if len(tables) != 2 {
		t.Fatalf("tables = %d, want 2 (Items + Totals)", len(tables))
	}
	// Items: header + 3 data rows, wrapped words merged.
	items := tables[0]
	if len(items.Rows) != 4 {
		t.Fatalf("Items rows = %d, want 4", len(items.Rows))
	}
	header := rowTexts(items.Rows[0])
	for _, want := range []string{"#", "Qty", "Unit Price", "Amount"} {
		if !slicesContain(header, want) {
			t.Errorf("Items header %v missing %q", header, want)
		}
	}
	if got := rowTexts(items.Rows[1]); !slicesEqual(got, []string{"1", "Consulting services", "10", "120.00", "1200.00"}) {
		t.Errorf("row 1 = %v, want wrap-merged [1 Consulting services 10 120.00 1200.00]", got)
	}
	// Totals: 4 rows, exact cells.
	totals := tables[1]
	if len(totals.Rows) != 4 {
		t.Fatalf("Totals rows = %d, want 4", len(totals.Rows))
	}
	wantRows := [][]string{
		{"Item", "Amount"},
		{"Subtotal", "2400.00"},
		{"VAT (19%)", "456.00"},
		{"Total Due", "2856.00"},
	}
	for i, w := range wantRows {
		if got := rowTexts(totals.Rows[i]); !slicesEqual(got, w) {
			t.Errorf("Totals row %d = %v, want %v", i, got, w)
		}
	}
	// note must not leak into any table
	for _, tb := range tables {
		for _, r := range tb.Rows {
			for _, c := range r.Cells {
				if strings.Contains(c.Text, "Payment") {
					t.Errorf("note leaked into table: %q", c.Text)
				}
			}
		}
	}
}
