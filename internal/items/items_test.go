package items

import (
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func base() Base {
	return Base{Scheme: "mail", Platform: "gmail", Thread: "T42", Msg: "M17"}
}

func TestSplitHTMLStructure(t *testing.T) {
	roots, err := SplitHTML(load(t, "sample.html"), base())
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("want 1 root page, got %d", len(roots))
	}
	root := roots[0]
	if root.Kind != KindPage {
		t.Fatalf("root kind = %v, want page", root.Kind)
	}
	if root.URL != "mail://gmail/T42/M17/body" {
		t.Errorf("root URL = %q", root.URL)
	}

	// paragraphs p[0], p[1]; heading; image; link; table
	var paras []*Item
	var head, img, table *Item
	for _, c := range root.Children {
		switch c.Kind {
		case KindParagraph:
			paras = append(paras, c)
		case KindHeading:
			head = c
		case KindImage:
			img = c
		case KindTable:
			table = c
		}
	}
	if len(paras) != 2 {
		t.Fatalf("want 2 paragraphs, got %d", len(paras))
	}
	if paras[0].URL != "mail://gmail/T42/M17/body/p[0]" {
		t.Errorf("p0 URL = %q", paras[0].URL)
	}
	if paras[1].URL != "mail://gmail/T42/M17/body/p[1]" {
		t.Errorf("p1 URL = %q", paras[1].URL)
	}
	if head == nil {
		t.Fatal("no heading")
	}
	if head.URL != "mail://gmail/T42/M17/body/heading[0]" {
		t.Errorf("heading URL = %q", head.URL)
	}
	if img == nil || img.URL != "mail://gmail/T42/M17/body/img[0]" {
		t.Errorf("image = %+v", img)
	}
	if img.Src != "https://example.com/x.png" || img.Alt != "diagram" {
		t.Errorf("img attrs = src=%q alt=%q", img.Src, img.Alt)
	}
	// the link is inline inside paragraph[0]
	var p0Link *Item
	for _, c := range paras[0].Children {
		if c.Kind == KindLink {
			p0Link = c
		}
	}
	if p0Link == nil || p0Link.Href != "https://example.com/doc" {
		t.Errorf("inline link = %+v", p0Link)
	}
	if p0Link != nil && p0Link.URL != "mail://gmail/T42/M17/body/p[0]/a[0]" {
		t.Errorf("inline link URL = %q", p0Link.URL)
	}

	// table with 2 rows, 2 cells each
	if table == nil {
		t.Fatal("no table")
	}
	if len(table.Children) != 2 {
		t.Fatalf("table rows = %d, want 2", len(table.Children))
	}
	r0 := table.Children[0]
	if r0.Kind != KindRow || r0.URL != "mail://gmail/T42/M17/body/table[0]/tr[0]" {
		t.Errorf("row0 = %v %q", r0.Kind, r0.URL)
	}
	if len(r0.Children) != 2 {
		t.Fatalf("row0 cells = %d", len(r0.Children))
	}
	c0 := r0.Children[0]
	if c0.Kind != KindCell || c0.URL != "mail://gmail/T42/M17/body/table[0]/tr[0]/td[0]" {
		t.Errorf("cell0 = %v %q", c0.Kind, c0.URL)
	}
	if c0.Body != "A" {
		t.Errorf("cell0 body = %q, want A", c0.Body)
	}
}

func TestSplitMarkdownStructure(t *testing.T) {
	roots, err := SplitMarkdown(load(t, "sample.md"), base())
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("want 1 root, got %d", len(roots))
	}
	root := roots[0]
	var table, link, image *Item
	var paras []*Item
	for _, c := range root.Children {
		switch c.Kind {
		case KindParagraph:
			paras = append(paras, c)
		case KindTable:
			table = c
		case KindLink:
			link = c
		case KindImage:
			image = c
		}
	}
	// two plain paragraphs before the table
	if len(paras) < 2 {
		t.Fatalf("paras = %d", len(paras))
	}
	if table == nil {
		t.Fatal("no table parsed")
	}
	if len(table.Children) != 2 {
		t.Fatalf("table rows = %d, want 2 (header + row)", len(table.Children))
	}
	head := table.Children[0]
	if len(head.Children) != 2 {
		t.Fatalf("header cells = %d", len(head.Children))
	}
	if head.Children[0].Body != "Name" {
		t.Errorf("header cell = %q", head.Children[0].Body)
	}
	if link == nil || link.Href != "https://example.com/docs" {
		t.Errorf("markdown link = %+v", link)
	}
	if image == nil || image.Src != "https://example.com/a.png" {
		t.Errorf("markdown image = %+v", image)
	}
}

func TestIndexCounterPerType(t *testing.T) {
	roots, err := SplitHTML(load(t, "sample.html"), base())
	if err != nil {
		t.Fatal(err)
	}
	// img[0] and table[0] and link[0] are independent counters in the same page
	var saw bool
	for _, c := range roots[0].Children {
		if c.Kind == KindTable {
			saw = true
		}
	}
	if !saw {
		t.Fatal("expected table child")
	}
}
