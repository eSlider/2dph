// Package selector implements the point-retrieval selector mini-language
// (AGENTS #100): a DOM/jQuery-like structural selector over a typed Item tree.
// Steps are separated by '>' (direct child) or whitespace (descendant), each
// step being an indexed segment, e.g. "p[3] > table[0] > tr[1] td[2]". An
// omitted index matches any node of that type at that level.
package selector

import (
	"fmt"
	"strings"

	"github.com/eSlider/2dph/internal/address"
	"github.com/eSlider/2dph/internal/items"
)

// step is one selector step plus its traversal relationship to the previous
// step. The first step is always a descendant search from the root.
type step struct {
	seg   address.Segment
	child bool // true when preceded by '>'
}

// Expr is a parsed selector expression.
type Expr struct {
	steps []step
}

// Parse parses a selector string into an Expr.
func Parse(s string) (*Expr, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("selector: empty expression")
	}
	expr := &Expr{}
	i := 0
	first, next, err := readStep(s, i)
	if err != nil {
		return nil, err
	}
	expr.steps = append(expr.steps, step{seg: first})
	i = next

	for i < len(s) {
		// consume whitespace between steps
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i >= len(s) {
			break
		}
		child := false
		if s[i] == '>' {
			child = true
			i++
			for i < len(s) && s[i] == ' ' {
				i++
			}
			if i >= len(s) {
				return nil, fmt.Errorf("selector: dangling '>' in %q", s)
			}
		}
		seg, next, err := readStep(s, i)
		if err != nil {
			return nil, err
		}
		expr.steps = append(expr.steps, step{seg: seg, child: child})
		i = next
	}
	return expr, nil
}

// readStep reads one "type[index]" token starting at i, returning the segment
// and the index just past the token.
func readStep(s string, i int) (address.Segment, int, error) {
	start := i
	for i < len(s) && isStepChar(s[i]) {
		i++
	}
	tok := s[start:i]
	seg, err := address.ParseSegment(tok)
	if err != nil {
		return address.Segment{}, i, fmt.Errorf("selector: %w", err)
	}
	return seg, i, nil
}

func isStepChar(c byte) bool {
	return c == '[' || c == ']' || c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// Apply resolves the expression against the item tree rooted at root and
// returns the matched nodes. A missing index matches any node of that type.
func (e *Expr) Apply(root *items.Item) ([]*items.Item, error) {
	if len(e.steps) == 0 {
		return nil, nil
	}
	current := descendantMatch(root, e.steps[0].seg)
	for _, st := range e.steps[1:] {
		var next []*items.Item
		for _, it := range current {
			if st.child {
				next = append(next, childMatch(it, st.seg)...)
			} else {
				next = append(next, descendantMatch(it, st.seg)...)
			}
		}
		current = next
	}
	return current, nil
}

// matches reports whether it matches seg by type and, when seg has an index,
// by the node's own per-parent index.
func matches(it *items.Item, seg address.Segment) bool {
	if it.Kind.String() != seg.Type {
		return false
	}
	if seg.HasIndex {
		return it.Seg.HasIndex && it.Seg.Index == seg.Index
	}
	return true
}

func childMatch(n *items.Item, seg address.Segment) []*items.Item {
	var out []*items.Item
	for _, c := range n.Children {
		if matches(c, seg) {
			out = append(out, c)
		}
	}
	return out
}

func descendantMatch(n *items.Item, seg address.Segment) []*items.Item {
	var out []*items.Item
	var walk func(*items.Item)
	walk = func(node *items.Item) {
		for _, c := range node.Children {
			if matches(c, seg) {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(n)
	return out
}
