package utils

import "strings"

// SafeSegment turns an arbitrary id/name into a safe single path segment:
// path separators and control characters are replaced, runs collapsed, and the
// result is truncated. It never returns an empty string for non-empty input, so
// callers can use it directly as a directory or file component.
func SafeSegment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	lastSep := true
	for _, r := range s {
		if r == '/' || r == '\\' || r < 0x20 || r == 0x7f {
			if !lastSep {
				b.WriteByte('-')
				lastSep = true
			}
			continue
		}
		b.WriteRune(r)
		lastSep = false
	}
	out := strings.Trim(b.String(), ".-")
	if out == "" {
		return "-"
	}
	return out
}
