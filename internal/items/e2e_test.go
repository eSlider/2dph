package items_test

import (
	"testing"

	"github.com/eSlider/2dph/internal/address"
	"github.com/eSlider/2dph/internal/items"
	"github.com/eSlider/2dph/internal/selector"
)

// TestEndToEnd proves the three pieces work together: split an HTML body into
// a typed Item tree, address a leaf via the selector mini-language, and verify
// the leaf's canonical content URL plus node/content IDs (AGENTS #100).
func TestEndToEnd(t *testing.T) {
	doc := `<html><body>
<h2>Summary</h2>
<p>First <a href="https://example.com/doc">link</a>.</p>
<table>
<tr><td>A</td><td>B</td></tr>
<tr><td>C</td><td>D</td><td>E</td></tr>
</table>
<p>Trailer.</p>
</body></html>`

	base := items.Base{Scheme: "mail", Platform: "gmail", Thread: "T42", Msg: "M17"}
	roots, err := items.SplitHTML(doc, base)
	if err != nil {
		t.Fatal(err)
	}

	expr, err := selector.Parse("table[0] > tr[1] > td[2]")
	if err != nil {
		t.Fatal(err)
	}
	got, err := expr.Apply(roots[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "E" {
		t.Fatalf("selector result = %d items", len(got))
	}
	leaf := got[0]

	wantURL := "mail://gmail/T42/M17/body/table[0]/tr[1]/td[2]"
	if leaf.URL != wantURL {
		t.Errorf("leaf URL = %q, want %q", leaf.URL, wantURL)
	}

	// node ID is the first 16 bytes of sha256(url); content ID is sha256(body).
	nid := address.NodeID(leaf.URL)
	if len(nid) != 32 {
		t.Errorf("node id length = %d", len(nid))
	}
	cid := address.ContentID(leaf.Body)
	if len(cid) != 64 {
		t.Errorf("content id length = %d", len(cid))
	}

	// parse the URL back and confirm it round-trips to the same leaf.
	p, err := address.Parse(leaf.URL)
	if err != nil {
		t.Fatal(err)
	}
	if p.Scheme != "mail" || p.Platform != "gmail" || p.Thread != "T42" || p.Msg != "M17" {
		t.Errorf("parse header = %+v", p)
	}
	if len(p.Segments) != 4 {
		t.Fatalf("parsed segments = %+v", p.Segments)
	}
	if p.Segments[len(p.Segments)-1].String() != "td[2]" {
		t.Errorf("leaf segment = %q", p.Segments[len(p.Segments)-1].String())
	}
}
