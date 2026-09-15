package docgraph

import (
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/contract"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

func TestRowToLeafFull(t *testing.T) {
	r := Row{
		Source:      "portals",
		Channel:     "o2",
		ContentHash: "abcdef1234567890",
		ExternalID:  "https://example.test/r.pdf",
		Title:       "Rechnung September",
		Filename:    "2026-09-01-o2-Rechnung-1.pdf",
		OOPath:      "external/o2/2026-09-01-o2-Rechnung-1.pdf",
		DocDate:     mustTime(t, "2026-09-01T08:00:00Z"),
		ObservedAt:  mustTime(t, "2026-09-13T10:00:00Z"),
	}
	lf, ok := RowToLeaf(r)
	if !ok {
		t.Fatal("want leaf")
	}
	if lf.ExternalID != "https://example.test/r.pdf" {
		t.Fatalf("external_id = %q", lf.ExternalID)
	}
	want := "Rechnung September\n\n2026-09-01-o2-Rechnung-1.pdf\n\nexternal/o2/2026-09-01-o2-Rechnung-1.pdf"
	if lf.Text != want {
		t.Fatalf("text = %q, want %q", lf.Text, want)
	}
	if lf.Source != "document" || lf.Kind != "document" || lf.Root != "info" {
		t.Fatalf("meta = %+v", lf)
	}
	if lf.How != "gator/document" || lf.Confidence != "confirmed" {
		t.Fatalf("how/confidence = %q/%q", lf.How, lf.Confidence)
	}
	if lf.Loc != "kind=document#v-abcdef12" {
		t.Fatalf("loc = %q", lf.Loc)
	}
	// doc_date wins over observed_at.
	if lf.ObservedAt != "2026-09-01T08:00:00Z" {
		t.Fatalf("observed_at = %q", lf.ObservedAt)
	}
	if err := lf.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRowToLeafObservedAtFallsBackToObservedAt(t *testing.T) {
	r := Row{
		Source:     "portals",
		ExternalID: "ext-1",
		Title:      "Rechnung",
		DocDate:    time.Time{}, // zero → use observed_at
		ObservedAt: mustTime(t, "2026-09-13T10:00:00Z"),
	}
	lf, ok := RowToLeaf(r)
	if !ok {
		t.Fatal("want leaf")
	}
	if lf.ObservedAt != "2026-09-13T10:00:00Z" {
		t.Fatalf("observed_at = %q", lf.ObservedAt)
	}
}

func TestRowToLeafSHA256Fallback(t *testing.T) {
	lf, ok := RowToLeaf(Row{Source: "portals", Title: "T", SHA256: "abc123"})
	if !ok {
		t.Fatal("want leaf")
	}
	if lf.ExternalID != "abc123" {
		t.Fatalf("external_id = %q", lf.ExternalID)
	}
}

func TestRowToLeafSkipsEmptyIdentity(t *testing.T) {
	// Title alone is not identity: no external_id, no sha256 → skip.
	if _, ok := RowToLeaf(Row{Source: "portals", Title: "no identity"}); ok {
		t.Fatal("row without external_id/sha256 must skip")
	}
}

func TestRowToLeafSkipsEmptyText(t *testing.T) {
	// external_id gives identity, but no title/filename/oo_path to index.
	if _, ok := RowToLeaf(Row{Source: "portals", ExternalID: "ext-1"}); ok {
		t.Fatal("content-less row must skip")
	}
}

func TestRowToLeafIdempotent(t *testing.T) {
	r := Row{Source: "portals", ExternalID: "ext-1", Title: "N", Filename: "n.pdf"}
	a, ok := RowToLeaf(r)
	if !ok {
		t.Fatal("want leaf")
	}
	b, ok := RowToLeaf(r)
	if !ok {
		t.Fatal("want leaf")
	}
	if a != b {
		t.Fatalf("same row must map to the same leaf: %+v vs %+v", a, b)
	}
	if a.ContentHash() != b.ContentHash() {
		t.Fatal("same row must yield the same ContentHash")
	}
	want := contract.Leaf{Source: "document", ExternalID: "ext-1", Kind: "document", Text: "N\n\nn.pdf"}
	if a.ContentHash() != want.ContentHash() {
		t.Fatal("ContentHash must match contract leaf fields")
	}
}

func TestRowToLeafDistinctExternalID(t *testing.T) {
	a, _ := RowToLeaf(Row{Source: "portals", ExternalID: "ext-a", Title: "N"})
	b, _ := RowToLeaf(Row{Source: "portals", ExternalID: "ext-b", Title: "N"})
	if a.ContentHash() == b.ContentHash() {
		t.Fatal("different external_id must yield different ContentHash")
	}
}

// TestMapRowRealColumns pins the mapper to the gator PR #140 read schema
// (query.DocumentsColumns): exact column names, no name/url/content_type/
// converted/fetched_at.
func TestMapRowRealColumns(t *testing.T) {
	m := map[string]any{
		"source":       "portals",
		"channel":      "o2",
		"dt":           mustTime(t, "2026-09-13T00:00:00Z"),
		"content_hash": "h1",
		"external_id":  "https://x/y.pdf",
		"title":        "Rechnung",
		"doc_date":     mustTime(t, "2026-09-01T08:00:00Z"),
		"filename":     "y.pdf",
		"mime":         "application/pdf",
		"size":         int64(4096),
		"sha256":       "abc",
		"oo_path":      "external/o2/y.pdf",
		"ref":          "https://x/y.pdf",
		"posted_at":    mustTime(t, "2026-09-01T08:00:00Z"),
		"observed_at":  mustTime(t, "2026-09-13T10:00:00Z"),
	}
	r, err := MapRow(m)
	if err != nil {
		t.Fatal(err)
	}
	if r.Source != "portals" || r.Channel != "o2" || r.ContentHash != "h1" {
		t.Fatalf("envelope = %+v", r)
	}
	if r.ExternalID != "https://x/y.pdf" || r.Title != "Rechnung" || r.MIME != "application/pdf" {
		t.Fatalf("meta = %+v", r)
	}
	if r.Filename != "y.pdf" || r.OOPath != "external/o2/y.pdf" || r.Ref != "https://x/y.pdf" {
		t.Fatalf("refs = %+v", r)
	}
	if r.SHA256 != "abc" || r.Size != 4096 {
		t.Fatalf("hash/size = %q/%d", r.SHA256, r.Size)
	}
	if r.Date.Format(time.RFC3339) != "2026-09-13T00:00:00Z" {
		t.Fatalf("dt = %v", r.Date)
	}
	if r.DocDate.Format(time.RFC3339) != "2026-09-01T08:00:00Z" {
		t.Fatalf("doc_date = %v", r.DocDate)
	}
	if r.ObservedAt.Format(time.RFC3339) != "2026-09-13T10:00:00Z" {
		t.Fatalf("observed_at = %v", r.ObservedAt)
	}
}

func TestMapRowsSkipsBadRow(t *testing.T) {
	rows, skipped, firstErr := MapRows([]map[string]any{
		{"source": "portals", "external_id": "ext-1"},
		{"source": "portals", "dt": "not-a-time"},
	})
	if skipped != 1 || firstErr == nil {
		t.Fatalf("skipped=%d firstErr=%v", skipped, firstErr)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
}
