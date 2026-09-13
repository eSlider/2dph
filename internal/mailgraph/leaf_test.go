package mailgraph

import (
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/contract"
)

func TestRowToLeafFull(t *testing.T) {
	r := Row{
		MessageID:   "msg-1@example.com",
		Subject:     "Hello",
		Body:        "world",
		Date:        mustTime(t, "2026-09-13T10:00:00Z"),
		ContentHash: "abcdef1234567890",
	}
	lf, ok := RowToLeaf(r)
	if !ok {
		t.Fatal("want leaf")
	}
	if lf.ExternalID != "msg-1@example.com" {
		t.Fatalf("external_id = %q", lf.ExternalID)
	}
	if lf.Text != "Hello\n\nworld" {
		t.Fatalf("text = %q", lf.Text)
	}
	if lf.Source != "mail" || lf.Kind != "mail" || lf.Root != "info" {
		t.Fatalf("meta = %+v", lf)
	}
	if lf.How != "gator/mail" {
		t.Fatalf("how = %q", lf.How)
	}
	if lf.Loc != "kind=mail#v-abcdef12" {
		t.Fatalf("loc = %q", lf.Loc)
	}
	if lf.ObservedAt != "2026-09-13T10:00:00Z" {
		t.Fatalf("observed_at = %q", lf.ObservedAt)
	}
	if err := lf.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRowToLeafSkipsEmpty(t *testing.T) {
	_, ok := RowToLeaf(Row{})
	if ok {
		t.Fatal("empty row should skip")
	}
	_, ok = RowToLeaf(Row{MessageID: "x@y.com", Subject: "  ", Body: ""})
	if ok {
		t.Fatal("content-less row should skip")
	}
}

func TestRowToLeafSubjectOnly(t *testing.T) {
	lf, ok := RowToLeaf(Row{MessageID: "a@b.com", Subject: "Only subject", Date: time.Now()})
	if !ok || lf.Text != "Only subject" {
		t.Fatalf("got ok=%v text=%q", ok, lf.Text)
	}
}

func TestRowToLeafContentHashDedupKey(t *testing.T) {
	a, ok := RowToLeaf(Row{MessageID: "same@id.com", Subject: "S", Body: "B"})
	if !ok {
		t.Fatal("want leaf")
	}
	b, ok := RowToLeaf(Row{MessageID: "other@id.com", Subject: "S", Body: "B"})
	if !ok {
		t.Fatal("want leaf")
	}
	if a.ContentHash() == b.ContentHash() {
		t.Fatal("different external_id must yield different ContentHash")
	}
	want := contract.Leaf{Source: "mail", ExternalID: "same@id.com", Kind: "mail", Text: "S\n\nB"}
	if a.ContentHash() != want.ContentHash() {
		t.Fatal("ContentHash must match contract leaf fields")
	}
}
