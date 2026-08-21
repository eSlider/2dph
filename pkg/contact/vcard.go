package contact

import (
	"io"
	"os"
	"strings"

	vcard "github.com/emersion/go-vcard"
)

// parseVCardFile parses every card in one .vcf/.vcard file into contacts.
// It accepts both vCard 2.1 and 3.0 (the decoder is lenient on both).
func parseVCardFile(path string) ([]Contact, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return parseVCard(f, path)
}

func parseVCard(r io.Reader, source string) ([]Contact, error) {
	// vCard 2.1 uses bare-type params ("TEL;HOME:...") which go-vcard (3.0/4.0
	// oriented) misparses as empty values. Detect 2.1 and normalize first.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	dec := vcard.NewDecoder(strings.NewReader(normalizeV21(string(data))))
	var out []Contact
	for {
		card, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A malformed card should not abort the whole file: skip it.
			continue
		}
		c := Contact{Source: source}
		if f := card.Get(vcard.FieldFormattedName); f != nil {
			c.FullName = strings.TrimSpace(f.Value)
		}
		if n := card.Get(vcard.FieldName); n != nil {
			parts := splitNameField(n.Value)
			if len(parts) > 1 {
				c.Family = parts[0]
				c.Given = parts[1]
			} else if len(parts) == 1 {
				c.Family = parts[0]
			}
		}
		if o := card.Get(vcard.FieldOrganization); o != nil {
			c.Org = strings.TrimSpace(strings.ReplaceAll(o.Value, ";", ", "))
		}
		if t := card.Get(vcard.FieldTitle); t != nil {
			c.Title = strings.TrimSpace(t.Value)
		}
		if p := card.Get(vcard.FieldPhoto); p != nil {
			c.Photo = strings.TrimSpace(p.Value)
		}
		for _, f := range card[vcard.FieldEmail] {
			if e := strings.TrimSpace(f.Value); e != "" {
				c.Emails = append(c.Emails, e)
			}
		}
		for _, f := range card[vcard.FieldTelephone] {
			if p := strings.TrimSpace(f.Value); p != "" {
				c.Phones = append(c.Phones, p)
			}
		}
		if c.HasIdentity() {
			out = append(out, c)
		}
	}
	return out, nil
}

// normalizeV21 rewrites vCard 2.1 bare-type parameters (";HOME:") into the
// 3.0 "TYPE=HOME" form go-vcard understands. It only touches params that are
// bare words without "=" and are not the group prefix (GROUP.;NAME:).
func normalizeV21(s string) string {
	if !strings.Contains(strings.ToUpper(s), "VERSION:2.1") {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		// Look for the first colon that separates name+params from the value.
		ci := strings.IndexByte(line, ':')
		if ci < 0 {
			out = append(out, line)
			continue
		}
		head, value := line[:ci], line[ci:]
		// Split head into property name and params, tracking groups like "item1.".
		segs := strings.Split(head, ";")
		var name string
		var params []string
		if strings.Contains(segs[0], ".") && len(segs) > 1 {
			// group.name
			name = segs[1]
			params = append(params, segs[0])
			params = append(params, segs[2:]...)
		} else {
			name = segs[0]
			params = append(params, segs[1:]...)
		}
		_ = name
		var outHead string
		for _, p := range params {
			if p == "" || strings.Contains(p, "=") {
				outHead += ";" + p
				continue
			}
			outHead += ";TYPE=" + p
		}
		out = append(out, name+outHead+value)
	}
	return strings.Join(out, "\n")
}

// splitNameField splits a vCard N value on unescaped semicolons. The N field
// is "Family;Given;Additional;Prefixes;Suffixes".
func splitNameField(v string) []string {
	var parts []string
	var cur strings.Builder
	esc := false
	for _, r := range v {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == ';':
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	parts = append(parts, cur.String())
	return parts
}
