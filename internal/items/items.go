// Package items implements granular content splitting (AGENTS #100): an
// html/markdown body is decomposed BEFORE insertion into a typed tree of
// Items (paragraph / heading / table / row / cell / image / link / page).
// Every Item carries its canonical content URL (internal/address) and its
// full path of indexed segments so a DOM/jQuery-like selector can address any
// leaf (see internal/selector).
package items

import (
	"fmt"
	"strings"

	"github.com/eSlider/2dph/internal/address"
	"golang.org/x/net/html"
)

// Kind is a named int type for the node type of an Item (AGENTS: enums are
// named int types, never loose strings at boundaries).
type Kind int

const (
	KindPage Kind = iota
	KindHeading
	KindParagraph
	KindTable
	KindRow
	KindCell
	KindImage
	KindLink
)

// segType maps a Kind to its URL/selector segment type. KindPage renders as
// "body" to match the canonical example mail://.../body/p[3].
func (k Kind) segType() string {
	switch k {
	case KindPage:
		return "body"
	case KindHeading:
		return "heading"
	case KindParagraph:
		return "p"
	case KindTable:
		return "table"
	case KindRow:
		return "tr"
	case KindCell:
		return "td"
	case KindImage:
		return "img"
	case KindLink:
		return "a"
	default:
		return "node"
	}
}

func (k Kind) String() string { return k.segType() }

// Base carries the scheme/platform/thread/msg prefix used to build each Item's
// canonical URL (internal/address).
type Base struct {
	Scheme   string
	Platform string
	Thread   string
	Msg      string
}

// Item is one node in the split tree. Path is the full list of indexed
// segments from the root (leaf last, e.g. body/p[3]/table[0]); URL is the
// canonical content URL for this node.
type Item struct {
	Kind     Kind
	Seg      address.Segment
	Path     []address.Segment
	URL      string
	Body     string
	Src      string // image src / link href target URL
	Alt      string
	Href     string
	Children []*Item
}

// buildURL computes the canonical URL for an item given base and path.
func buildURL(base Base, path []address.Segment) (string, error) {
	return address.New(base.Scheme, base.Platform, base.Thread, base.Msg, path, "")
}

// newChild creates an item as child of parent, assigning its per-type index
// and full path + URL.
func newChild(base Base, parent *Item, counters map[string]int, kind Kind) (*Item, error) {
	typ := kind.segType()
	idx := counters[typ]
	counters[typ] = idx + 1
	seg := address.Segment{Type: typ, Index: idx, HasIndex: true}
	path := make([]address.Segment, 0, len(parent.Path)+1)
	path = append(path, parent.Path...)
	path = append(path, seg)
	u, err := buildURL(base, path)
	if err != nil {
		return nil, err
	}
	return &Item{Kind: kind, Seg: seg, Path: path, URL: u}, nil
}

// newPage creates the root page item ("body", unindexed).
func newPage(base Base) (*Item, error) {
	u, err := buildURL(base, []address.Segment{{Type: "body"}})
	if err != nil {
		return nil, err
	}
	return &Item{Kind: KindPage, Seg: address.Segment{Type: "body"}, Path: []address.Segment{{Type: "body"}}, URL: u}, nil
}

// SplitHTML parses an HTML body into a typed Item tree. Returns the top-level
// page item(s); for a normal document that is exactly one.
func SplitHTML(raw string, base Base) ([]*Item, error) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("items: parse html: %w", err)
	}
	page, err := newPage(base)
	if err != nil {
		return nil, err
	}
	counters := map[string]int{}
	body := findBody(doc)
	if body != nil {
		for c := body.FirstChild; c != nil; c = c.NextSibling {
			if err := convertElement(c, base, page, counters); err != nil {
				return nil, err
			}
		}
	}
	return []*Item{page}, nil
}

// findBody locates the <body> element in a parsed document.
func findBody(doc *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "body" {
			found = n
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return found
}

// convertElement converts an element node into zero or more Items appended to
// parent.
func convertElement(n *html.Node, base Base, parent *Item, counters map[string]int) error {
	if n == nil || n.Type != html.ElementNode {
		return nil
	}
	tag := strings.ToLower(n.Data)
	switch {
	case tag == "script" || tag == "style" || tag == "head" || tag == "title" || tag == "meta" || tag == "link":
		return nil // non-content
	case tag == "h1" || tag == "h2" || tag == "h3" || tag == "h4" || tag == "h5" || tag == "h6":
		it, err := newChild(base, parent, counters, KindHeading)
		if err != nil {
			return err
		}
		it.Body = textContentExcluding(n, nil)
		parent.Children = append(parent.Children, it)
		return nil
	case tag == "img":
		it, err := newChild(base, parent, counters, KindImage)
		if err != nil {
			return err
		}
		it.Src = attr(n, "src")
		it.Alt = attr(n, "alt")
		parent.Children = append(parent.Children, it)
		return nil
	case tag == "a":
		it, err := newChild(base, parent, counters, KindLink)
		if err != nil {
			return err
		}
		it.Href = attr(n, "href")
		it.Body = strings.TrimSpace(textContentExcluding(n, nil))
		parent.Children = append(parent.Children, it)
		return nil
	case tag == "table":
		return convertTable(n, base, parent, counters)
	case tag == "p" || tag == "div" || tag == "section" || tag == "article" ||
		tag == "blockquote" || tag == "li" || tag == "ul" || tag == "ol" ||
		tag == "dl" || tag == "dd" || tag == "dt" || tag == "pre":
		it, err := newChild(base, parent, counters, KindParagraph)
		if err != nil {
			return err
		}
		it.Body = strings.TrimSpace(textContentExcluding(n, []string{"a", "img"}))
		parent.Children = append(parent.Children, it)
		// inline links/images become children of this paragraph
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if err := convertInline(c, base, it, counters); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		// generic container: recurse children into same parent
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := convertElement(c, base, parent, counters); err != nil {
				return err
			}
		}
		return nil
	}
}

// convertInline adds inline elements (links/images) nested inside a paragraph
// as children of that paragraph.
func convertInline(n *html.Node, base Base, parent *Item, counters map[string]int) error {
	tag := strings.ToLower(n.Data)
	switch tag {
	case "a":
		it, err := newChild(base, parent, counters, KindLink)
		if err != nil {
			return err
		}
		it.Href = attr(n, "href")
		it.Body = strings.TrimSpace(textContentExcluding(n, nil))
		parent.Children = append(parent.Children, it)
	case "img":
		it, err := newChild(base, parent, counters, KindImage)
		if err != nil {
			return err
		}
		it.Src = attr(n, "src")
		it.Alt = attr(n, "alt")
		parent.Children = append(parent.Children, it)
	default:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode {
				if err := convertInline(c, base, parent, counters); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// convertTable builds the table/row/cell subtree.
func convertTable(n *html.Node, base Base, parent *Item, counters map[string]int) error {
	t, err := newChild(base, parent, counters, KindTable)
	if err != nil {
		return err
	}
	parent.Children = append(parent.Children, t)
	rowCounters := map[string]int{}
	var walkRows func(*html.Node) error
	walkRows = func(node *html.Node) error {
		if node.Type != html.ElementNode {
			return nil
		}
		tag := strings.ToLower(node.Data)
		if tag == "tr" {
			row, err := newChild(base, t, rowCounters, KindRow)
			if err != nil {
				return err
			}
			t.Children = append(t.Children, row)
			cellCounters := map[string]int{}
			for c := node.FirstChild; c != nil; c = c.NextSibling {
				if c.Type == html.ElementNode && (strings.ToLower(c.Data) == "td" || strings.ToLower(c.Data) == "th") {
					cell, err := newChild(base, row, cellCounters, KindCell)
					if err != nil {
						return err
					}
					cell.Body = strings.TrimSpace(textContentExcluding(c, nil))
					row.Children = append(row.Children, cell)
				}
			}
			return nil
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			if err := walkRows(c); err != nil {
				return err
			}
		}
		return nil
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if err := walkRows(c); err != nil {
			return err
		}
	}
	return nil
}

// textContentExcluding concatenates the text of all descendants of n, skipping
// entire subtrees rooted at any tag listed in skip (a/img children are
// represented as their own Items).
func textContentExcluding(n *html.Node, skip []string) string {
	skipSet := make(map[string]struct{}, len(skip))
	for _, s := range skip {
		skipSet[s] = struct{}{}
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if _, ok := skipSet[strings.ToLower(node.Data)]; ok {
				return
			}
		}
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c)
	}
	return b.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
