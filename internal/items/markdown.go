package items

import (
	"regexp"
	"strings"
)

var (
	mdImageRe = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)\s]+)\)$`)
	mdLinkRe  = regexp.MustCompile(`^\[([^\]]*)\]\(([^)\s]+)\)$`)
	mdRowRe   = regexp.MustCompile(`\|`)
	mdSepRe   = regexp.MustCompile(`^\s*\|?[\s:|-]+\|?$`)
)

// SplitMarkdown splits a markdown body into a typed Item tree (paragraphs,
// pipe-tables, standalone image/link blocks). Indices are per-type within the
// page.
func SplitMarkdown(raw string, base Base) ([]*Item, error) {
	page, err := newPage(base)
	if err != nil {
		return nil, err
	}
	counters := map[string]int{}
	blocks := splitBlocks(raw)
	for i := 0; i < len(blocks); i++ {
		block := strings.TrimSpace(blocks[i])
		if block == "" {
			continue
		}
		// Detect a pipe table: consecutive lines containing '|', a separator
		// line, then at least one data row.
		lines := strings.Split(block, "\n")
		if len(lines) >= 2 && mdSepRe.MatchString(lines[1]) && containsPipe(lines[0]) {
			// collect subsequent data rows
			dataRows := [][]string{}
			for _, r := range lines[2:] {
				if strings.TrimSpace(r) == "" {
					continue
				}
				dataRows = append(dataRows, splitRow(r))
			}
			header := splitRow(lines[0])
			if err := addTable(base, page, counters, header, dataRows); err != nil {
				return nil, err
			}
			continue
		}
		if err := addBlock(base, page, counters, block); err != nil {
			return nil, err
		}
	}
	return []*Item{page}, nil
}

// splitBlocks splits a markdown document into blank-line-delimited blocks.
func splitBlocks(raw string) []string {
	norm := strings.ReplaceAll(raw, "\r\n", "\n")
	return strings.Split(norm, "\n\n")
}

func containsPipe(s string) bool { return strings.Contains(s, "|") }

func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func addTable(base Base, page *Item, counters map[string]int, header []string, rows [][]string) error {
	t, err := newChild(base, page, counters, KindTable)
	if err != nil {
		return err
	}
	page.Children = append(page.Children, t)
	rowCounters := map[string]int{}
	addRow := func(vals []string) error {
		row, err := newChild(base, t, rowCounters, KindRow)
		if err != nil {
			return err
		}
		t.Children = append(t.Children, row)
		cellCounters := map[string]int{}
		for _, v := range vals {
			cell, err := newChild(base, row, cellCounters, KindCell)
			if err != nil {
				return err
			}
			cell.Body = v
			row.Children = append(row.Children, cell)
		}
		return nil
	}
	if err := addRow(header); err != nil {
		return err
	}
	for _, r := range rows {
		if err := addRow(r); err != nil {
			return err
		}
	}
	return nil
}

func addBlock(base Base, page *Item, counters map[string]int, block string) error {
	// standalone image block
	if m := mdImageRe.FindStringSubmatch(block); m != nil {
		it, err := newChild(base, page, counters, KindImage)
		if err != nil {
			return err
		}
		it.Alt = m[1]
		it.Src = m[2]
		page.Children = append(page.Children, it)
		return nil
	}
	// standalone link block
	if m := mdLinkRe.FindStringSubmatch(block); m != nil {
		it, err := newChild(base, page, counters, KindLink)
		if err != nil {
			return err
		}
		it.Body = m[1]
		it.Href = m[2]
		page.Children = append(page.Children, it)
		return nil
	}
	it, err := newChild(base, page, counters, KindParagraph)
	if err != nil {
		return err
	}
	it.Body = block
	page.Children = append(page.Children, it)
	return nil
}
