package docbody

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/docgraph"
)

type fakeFetcher struct {
	data []byte
	err  error
	got  int
}

func (f *fakeFetcher) Fetch(ctx context.Context, r docgraph.Row) ([]byte, error) {
	f.got++
	return f.data, f.err
}

type fakeExtractor struct {
	text string
	err  error
	got  int
}

func (f *fakeExtractor) Extract(ctx context.Context, r docgraph.Row, data []byte) (string, error) {
	f.got++
	return f.text, f.err
}

func TestProviderBody(t *testing.T) {
	f := &fakeFetcher{data: []byte("%PDF-1.4 body")}
	e := &fakeExtractor{text: "Betrag 123,45 EUR"}
	p := Provider{Fetch: f, Extract: e}
	got, err := p.Body(context.Background(), docgraph.Row{Filename: "r.pdf", MIME: "application/pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Betrag 123,45 EUR" {
		t.Fatalf("body = %q", got)
	}
	if f.got != 1 || e.got != 1 {
		t.Fatalf("fetch/extract calls = %d/%d", f.got, e.got)
	}
}

func TestProviderNonPDFSkipsFetch(t *testing.T) {
	f := &fakeFetcher{data: []byte("x")}
	e := &fakeExtractor{text: "should not run"}
	p := Provider{Fetch: f, Extract: e}
	got, err := p.Body(context.Background(), docgraph.Row{Filename: "notes.txt", MIME: "text/plain"})
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
	if f.got != 0 || e.got != 0 {
		t.Fatalf("non-pdf must not fetch/extract: %d/%d", f.got, e.got)
	}
}

func TestProviderEmptyBody(t *testing.T) {
	p := Provider{Fetch: &fakeFetcher{}, Extract: &fakeExtractor{text: "unused"}}
	got, err := p.Body(context.Background(), docgraph.Row{Filename: "r.pdf"})
	if err != nil || got != "" {
		t.Fatalf("empty bytes: got %q, %v", got, err)
	}
}

func TestProviderPropagatesError(t *testing.T) {
	p := Provider{Fetch: &fakeFetcher{err: errors.New("no oo")}, Extract: &fakeExtractor{}}
	_, err := p.Body(context.Background(), docgraph.Row{Filename: "r.pdf"})
	if err == nil || !strings.Contains(err.Error(), "no oo") {
		t.Fatalf("err = %v", err)
	}
}

func TestProviderNilHalvesNoop(t *testing.T) {
	if got, err := (Provider{}).Body(context.Background(), docgraph.Row{Filename: "r.pdf"}); got != "" || err != nil {
		t.Fatalf("nil provider: %q, %v", got, err)
	}
}

func TestIsPDF(t *testing.T) {
	cases := []struct {
		r    docgraph.Row
		want bool
	}{
		{docgraph.Row{MIME: "application/pdf"}, true},
		{docgraph.Row{Filename: "Rechnung.PDF"}, true},
		{docgraph.Row{Filename: "rechnung.pdf"}, true},
		{docgraph.Row{Filename: "notes.txt", MIME: "text/plain"}, false},
		{docgraph.Row{}, false},
	}
	for _, c := range cases {
		if got := IsPDF(c.r); got != c.want {
			t.Errorf("IsPDF(%+v) = %v, want %v", c.r, got, c.want)
		}
	}
}
