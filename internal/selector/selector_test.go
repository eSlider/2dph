package selector

import (
	"testing"

	"github.com/eSlider/2dph/internal/address"
	"github.com/eSlider/2dph/internal/items"
)

// buildTree constructs the canonical nesting used by #100:
//
//	body
//	  p[0], p[1], p[2], p[3]
//	    p[3].table[0]
//	      tr[0] -> td[0], td[1]
//	      tr[1] -> td[0], td[1], td[2]
func buildTree() *items.Item {
	page := &items.Item{Kind: items.KindPage, Seg: address.Segment{Type: "body"}}
	for i := 0; i < 4; i++ {
		p := &items.Item{Kind: items.KindParagraph, Seg: address.Segment{Type: "p", Index: i, HasIndex: true}}
		page.Children = append(page.Children, p)
		if i == 3 {
			table := &items.Item{Kind: items.KindTable, Seg: address.Segment{Type: "table", Index: 0, HasIndex: true}}
			p.Children = append(p.Children, table)
			tr0 := &items.Item{Kind: items.KindRow, Seg: address.Segment{Type: "tr", Index: 0, HasIndex: true}}
			table.Children = append(table.Children, tr0)
			tr0.Children = append(tr0.Children,
				&items.Item{Kind: items.KindCell, Seg: address.Segment{Type: "td", Index: 0, HasIndex: true}, Body: "x"},
				&items.Item{Kind: items.KindCell, Seg: address.Segment{Type: "td", Index: 1, HasIndex: true}, Body: "y"},
			)
			tr1 := &items.Item{Kind: items.KindRow, Seg: address.Segment{Type: "tr", Index: 1, HasIndex: true}}
			table.Children = append(table.Children, tr1)
			tr1.Children = append(tr1.Children,
				&items.Item{Kind: items.KindCell, Seg: address.Segment{Type: "td", Index: 0, HasIndex: true}, Body: "a"},
				&items.Item{Kind: items.KindCell, Seg: address.Segment{Type: "td", Index: 1, HasIndex: true}, Body: "b"},
				&items.Item{Kind: items.KindCell, Seg: address.Segment{Type: "td", Index: 2, HasIndex: true}, Body: "c"},
			)
		}
	}
	return page
}

func sel(t *testing.T, s string) *Expr {
	t.Helper()
	e, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return e
}

func bodies(items []*items.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Body)
	}
	return out
}

func TestApplyCanonical(t *testing.T) {
	root := buildTree()
	got, err := sel(t, "p[3] > table[0] > tr[1] td[2]").Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "c" {
		t.Fatalf("want single cell 'c', got %v", bodies(got))
	}
}

func TestApplyAllChildSeparators(t *testing.T) {
	root := buildTree()
	got, err := sel(t, "p[3] > table[0] > tr[1] > td[2]").Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "c" {
		t.Fatalf("want 'c', got %v", bodies(got))
	}
}

func TestApplyParagraph(t *testing.T) {
	root := buildTree()
	got, err := sel(t, "p[0]").Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 paragraph, got %d", len(got))
	}
}

func TestApplyDescendantFromRoot(t *testing.T) {
	root := buildTree()
	got, err := sel(t, "tr[1] td[2]").Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Body != "c" {
		t.Fatalf("want 'c', got %v", bodies(got))
	}
}

func TestApplyMissingIndex(t *testing.T) {
	root := buildTree()
	got, err := sel(t, "p").Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("want 4 paragraphs, got %d", len(got))
	}
	all, err := sel(t, "p > table").Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("want 1 table, got %d", len(all))
	}
}

func TestApplyNoMatch(t *testing.T) {
	root := buildTree()
	got, err := sel(t, "p[99]").Apply(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 matches, got %d", len(got))
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",
		"   ",
		"p[3] >",
		"p[3] >> td",
		"> p",
		"p >",
		"p[3] > td >",
		"p[x]",
		"3",
		"p[]",
		"p[3] td[",
	}
	for _, s := range bad {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): expected error", s)
		}
	}
}

func TestParseGoodGrammar(t *testing.T) {
	good := []string{
		"p[3]",
		"p",
		"p[3] > table[0]",
		"p[3] > table[0] > tr[1] td[2]",
		"p > table > tr td",
	}
	for _, s := range good {
		if _, err := Parse(s); err != nil {
			t.Errorf("Parse(%q): unexpected error %v", s, err)
		}
	}
}
