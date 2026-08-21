package main

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Minimal Thunderbird address book (.mab, Mork format) parser.
//
// Mork stores a column dictionary in a `(a=c)` group (e.g. "(83=FirstName)")
// followed by row groups of `(HEX=value)` cells. This parser is deliberately
// tolerant: it scans the file for `(HEX=value)` pairs, maps hex column ids
// through the dictionary, groups cells into records (a new record begins at a
// FirstName cell), and extracts the identity fields we care about.
//
// Note: some real-world Thunderbird .mab files use an alias-table scheme with
// variable-length cell ids that map through a second dictionary; those need a
// full Mork parser. This minimal reader covers the common/plain case.
// Values may carry Mork text escapes ("^HH" hex bytes, "$$" etc.).

var morkCellRe = regexp.MustCompile(`\(([0-9A-Fa-f]{2})=((?:[^)\\]|\\.)*)\)`)

var morkKnown = map[string]string{
	"83": "FirstName",
	"84": "LastName",
	"87": "DisplayName",
	"89": "PrimaryEmail",
	"8B": "SecondEmail",
	"8F": "WorkPhone",
	"90": "HomePhone",
	"93": "CellularNumber",
	"A5": "JobTitle",
	"A7": "Company",
}

type morkCell struct{ col, val string }

func parseMAB(path string) ([]Contact, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cols := map[string]string{} // hex id -> column name
	var cells []morkCell
	for _, m := range morkCellRe.FindAllStringSubmatch(string(raw), -1) {
		id := strings.ToUpper(m[1])
		val := morkUnquote(m[2])
		name := cols[id]
		if name == "" {
			if k, known := morkKnown[id]; known {
				name = k
			}
		}
		// Column definition if the value equals the known column name.
		if k, known := morkKnown[id]; known && strings.EqualFold(val, k) {
			cols[id] = k
			continue
		}
		if name == "" {
			continue
		}
		cells = append(cells, morkCell{name, val})
	}

	// Group cells into records: a FirstName cell starts a new record.
	var groups [][]morkCell
	var cur []morkCell
	for _, c := range cells {
		if c.col == "FirstName" && len(cur) > 0 {
			groups = append(groups, cur)
			cur = nil
		}
		cur = append(cur, c)
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}

	var out []Contact
	for _, g := range groups {
		c := Contact{Source: path}
		for _, cell := range g {
			switch cell.col {
			case "FirstName":
				c.Given = cell.val
			case "LastName":
				c.Family = cell.val
			case "DisplayName":
				c.FullName = cell.val
			case "PrimaryEmail", "SecondEmail":
				c.Emails = append(c.Emails, cell.val)
			case "WorkPhone", "HomePhone", "CellularNumber":
				c.Phones = append(c.Phones, cell.val)
			case "JobTitle":
				c.Title = cell.val
			case "Company":
				c.Org = cell.val
			}
		}
		if c.HasIdentity() {
			out = append(out, c)
		}
	}
	return out, nil
}

// morkUnquote decodes Mork text escapes. "\\" -> literal char, "^HH" and
// "$HH" -> byte HH.
func morkUnquote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' {
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
			continue
		}
		if (c == '^' || c == '$') && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 2
				continue
			}
		}
		b.WriteByte(c)
	}
	return strings.TrimSpace(b.String())
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
