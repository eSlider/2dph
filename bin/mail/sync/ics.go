package sync

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	ics "github.com/arran4/golang-ical"
	"golang.org/x/text/encoding/charmap"
)

// ICSToMarkdown parses a VCALENDAR/VEVENT payload and renders a compact
// structured markdown block: what / when / where / organizer / attendees.
// Returns the raw text when the payload is not a calendar.
func ICSToMarkdown(data []byte) string {
	data = normalizeEncoding(data)
	cal, err := ics.ParseCalendar(strings.NewReader(string(data)))
	if err != nil {
		return normalizeMarkdown(string(data))
	}
	method := ""
	for _, p := range cal.CalendarProperties {
		if p.IANAToken == string(ics.ComponentPropertyMethod) {
			method = p.Value
			break
		}
	}
	method = strings.TrimSpace(method)
	var out []string
	for _, ev := range cal.Events() {
		summary := strings.TrimSpace(propValue(ev, ics.ComponentPropertySummary))
		if summary != "" {
			out = append(out, "# "+summary)
		}
		if when := eventWhen(ev); when != "" {
			out = append(out, "- **When:** "+when)
		}
		if loc := strings.TrimSpace(propValue(ev, ics.ComponentPropertyLocation)); loc != "" {
			out = append(out, "- **Where:** "+loc)
		}
		if desc := strings.TrimSpace(stripHTML(propValue(ev, ics.ComponentPropertyDescription))); desc != "" {
			out = append(out, "- **What:** "+desc)
		}
		if org := propValue(ev, ics.ComponentPropertyOrganizer); org != "" {
			out = append(out, "- **Organizer:** "+attendeeFmt(org))
		}
		for _, a := range ev.Attendees() {
			cn := strings.TrimSpace(firstParam(a.ICalParameters, "CN"))
			partstat := string(a.ParticipationStatus())
			name := cn
			if name == "" {
				name = a.Email()
			}
			line := name
			if email := a.Email(); email != "" && email != name {
				line = name + " <" + email + ">"
			}
			if partstat != "" && !strings.EqualFold(partstat, "NEEDS-ACTION") {
				line += " (" + strings.Title(strings.ToLower(strings.ReplaceAll(partstat, "_", " "))) + ")"
			}
			out = append(out, "- **Attendee:** "+line)
		}
	}
	if len(out) == 0 {
		return normalizeMarkdown(string(data))
	}
	if method != "" {
		out = append([]string{"*Calendar method: " + method + "*"}, out...)
	}
	return normalizeMarkdown(strings.Join(out, "\n\n"))
}

func eventWhen(ev *ics.VEvent) string {
	start, errStart := ev.GetStartAt()
	end, errEnd := ev.GetEndAt()
	// All-day events: golang-ical has dedicated getters.
	if errStart != nil {
		if allDay, err := ev.GetAllDayStartAt(); err == nil {
			start = allDay
			errStart = nil
		}
	}
	if errEnd != nil {
		if allDay, err := ev.GetAllDayEndAt(); err == nil {
			end = allDay
			errEnd = nil
		}
	}
	if errStart != nil {
		// Non-IANA TZID (e.g. "W. Europe Standard Time"): parse the raw
		// property text instead of failing.
		return rawWhen(ev)
	}
	if errEnd != nil || end.Equal(start) {
		return dtFmt(start)
	}
	return dtFmt(start) + " → " + dtFmt(end)
}

// rawWhen parses DTSTART/DTEND property values that golang-ical cannot resolve
// because the TZID is not an IANA zone. Formats: 20260812T120000 or 20260812.
func rawWhen(ev *ics.VEvent) string {
	start := rawPropValue(ev, ics.ComponentPropertyDtStart)
	end := rawPropValue(ev, ics.ComponentPropertyDtEnd)
	if start == "" {
		return ""
	}
	if end == "" || end == start {
		return rawDTFmt(start)
	}
	return rawDTFmt(start) + " → " + rawDTFmt(end)
}

func rawPropValue(ev *ics.VEvent, prop ics.ComponentProperty) string {
	p := ev.GetProperty(prop)
	if p == nil {
		return ""
	}
	return p.Value
}

// rawDTFmt turns 20260812T120000 into 2026-08-12 12:00; 20260812 into 2026-08-12.
func rawDTFmt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 8 && isDigits(s[:8]) {
		y, m, d := s[:4], s[4:6], s[6:8]
		if len(s) > 8 && (s[8] == 'T' || s[8] == 't') && len(s) >= 15 && isDigits(s[9:15]) {
			h, mi := s[9:11], s[11:13]
			return fmt.Sprintf("%s-%s-%s %s:%s", y, m, d, h, mi)
		}
		return fmt.Sprintf("%s-%s-%s", y, m, d)
	}
	return s
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// dtFmt renders a time as local "2006-01-02 15:04" (tz label when meaningful).
func dtFmt(t time.Time) string {
	loc := t.Local()
	label := ""
	if loc.Location() != time.Local {
		label = " " + loc.Location().String()
	}
	return loc.Format("2006-01-02 15:04") + label
}

// propertyGetter is satisfied by both *ics.Calendar and *ics.VEvent.
type propertyGetter interface {
	GetProperty(ics.ComponentProperty) *ics.IANAProperty
}

func propValue(ev propertyGetter, prop ics.ComponentProperty) string {
	p := ev.GetProperty(prop)
	if p == nil {
		return ""
	}
	return p.Value
}

func firstParam(params map[string][]string, key string) string {
	if vs, ok := params[key]; ok && len(vs) > 0 {
		return vs[0]
	}
	return ""
}

func attendeeFmt(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.Index(raw, ":"); i >= 0 {
		raw = raw[i+1:]
	}
	return raw
}

// stripHTML removes tags and decodes entities from an ics DESCRIPTION that may
// carry HTML (Outlook/Exchange style), keeping text lines readable.
func stripHTML(s string) string {
	if !strings.Contains(s, "<") {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		var b strings.Builder
		depth := 0
		for j := 0; j < len(l); j++ {
			c := l[j]
			if c == '<' {
				if j+1 < len(l) && l[j+1] == '/' {
					depth--
				} else {
					depth++
				}
				for j < len(l) && l[j] != '>' {
					j++
				}
				continue
			}
			if c == '>' {
				continue
			}
			if depth == 0 {
				b.WriteByte(c)
			}
		}
		lines[i] = strings.TrimSpace(b.String())
	}
	return strings.Join(lines, "\n")
}

// normalizeMarkdown collapses blank-line runs and strips control chars.
func normalizeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	for _, ch := range []string{"\ufeff", "\u200b", "\u034f", "\u00ad", "\u2007", "\u2008", "\u200a", "\u2002"} {
		s = strings.ReplaceAll(s, ch, "")
	}
	lines := strings.Split(s, "\n")
	var out []string
	blank := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// normalizeEncoding re-encodes legacy single-byte text as UTF-8. ICS files
// exported by some portals are Latin-1 (e.g. "N\xfcrnberg"); golang-ical
// passes the bytes through, producing invalid UTF-8 in the markdown output.
// Valid UTF-8 is returned untouched.
func normalizeEncoding(data []byte) []byte {
	if utf8.Valid(data) {
		return data
	}
	dec := charmap.ISO8859_1.NewDecoder()
	out, err := dec.Bytes(data)
	if err != nil {
		return data
	}
	return out
}

var _ = fmt.Sprintf // keep fmt import if helpers change
