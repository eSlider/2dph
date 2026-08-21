package contact

import (
	"encoding/csv"
	"io"
	"os"
	"regexp"
	"strings"
)

var emailColRe = regexp.MustCompile(`^E-mail\s*\d+\s*-\s*Value$`)
var phoneColRe = regexp.MustCompile(`^Phone\s*\d+\s*-\s*Value$`)

// parseGoogleCSV reads a Google Contacts CSV export. Columns are matched by
// header name (the export uses "E-mail 1 - Value", "Phone 1 - Value", etc.).
func parseGoogleCSV(path string) ([]Contact, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	cols := map[string]int{}
	for i, h := range header {
		cols[strings.TrimSpace(h)] = i
	}
	idx := func(name string) (int, bool) {
		i, ok := cols[name]
		return i, ok
	}

	var out []Contact
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		cell := func(name string) string {
			i, ok := idx(name)
			if !ok || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		c := Contact{
			FullName: cell("Name"),
			Given:    cell("Given Name"),
			Family:   cell("Family Name"),
			Org:      cell("Organization 1 - Name"),
			Title:    cell("Organization 1 - Title"),
			Photo:    cell("Photo"),
			Source:   path,
		}
		// Dynamic columns: any E-mail N - Value / Phone N - Value header.
		for i, h := range header {
			h = strings.TrimSpace(h)
			if i >= len(rec) {
				continue
			}
			v := strings.TrimSpace(rec[i])
			if v == "" {
				continue
			}
			switch {
			case emailColRe.MatchString(h):
				c.Emails = append(c.Emails, v)
			case phoneColRe.MatchString(h):
				c.Phones = append(c.Phones, v)
			}
		}
		if c.HasIdentity() {
			out = append(out, c)
		}
	}
	return out, nil
}
