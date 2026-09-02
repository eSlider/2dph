package incubator

import (
	"encoding/base64"
	"strings"
	"unicode/utf16"
)

// mutf7Alphabet is the RFC 3501 modified-UTF-7 base64 alphabet: standard
// base64 with '/' replaced by ',' (and no padding).
const mutf7Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+,"

var mutf7Enc = base64.NewEncoding(mutf7Alphabet).WithPadding(base64.NoPadding)

// DecodeUTF7 decodes an RFC 3501 modified UTF-7 (IMAP mailbox name encoding)
// segment, used by the corpus export tool for on-disk folder names with
// non-ASCII characters (observed: "Landesbetrieb_Mobilit&AOQ-t_..." for
// "Mobilitäts..."). Names without '&' pass through unchanged; malformed runs
// (stray '&' without a terminating '-') are kept literally — a folder label
// must never fail the import.
func DecodeUTF7(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := strings.IndexByte(s[i+1:], '-')
		if j < 0 { // unterminated run — keep the '&' literally
			b.WriteString(s[i:])
			break
		}
		seg := s[i+1 : i+1+j]
		i += 1 + j + 1
		if seg == "" { // "&-" is a literal ampersand
			b.WriteByte('&')
			continue
		}
		raw, err := mutf7Enc.DecodeString(seg)
		if err != nil { // not valid MUTF-7 — keep the segment as-is
			b.WriteByte('&')
			b.WriteString(seg)
			b.WriteByte('-')
			continue
		}
		units := make([]uint16, 0, len(raw)/2)
		for k := 0; k+1 < len(raw); k += 2 {
			units = append(units, uint16(raw[k])<<8|uint16(raw[k+1]))
		}
		b.WriteString(string(utf16.Decode(units)))
	}
	return b.String()
}
