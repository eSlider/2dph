// Package address implements canonical content URL addressing for the ETL
// layer (AGENTS #100). Every extracted node gets a stable URL of the form
//
//	scheme://platform/thread/msg/path-segments[#anchor]
//
// e.g. mail://gmail/T42/M17/body/p[3]/table[0]#r2,c5. The node ID is the first
// 16 bytes of sha256(url); content integrity is a separate full sha256(body).
package address

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Segment is one path segment: an optional indexed node type selector
// ("p[3]", "table[0]", "body"). An omitted index selects "any" of that type.
type Segment struct {
	Type     string
	Index    int
	HasIndex bool
}

// String renders the segment in the URL grammar: "type" or "type[index]".
func (s Segment) String() string {
	if !s.HasIndex {
		return s.Type
	}
	return fmt.Sprintf("%s[%d]", s.Type, s.Index)
}

var (
	segRe    = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*)(?:\[([0-9]+)\])?$`)
	schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*$`)
	compRe   = regexp.MustCompile(`^[^/:#]+$`)
	anchorRe = regexp.MustCompile(`^[^/#]+$`)
)

// ParseSegment parses a single segment from its string form.
func ParseSegment(s string) (Segment, error) {
	m := segRe.FindStringSubmatch(s)
	if m == nil {
		return Segment{}, fmt.Errorf("address: invalid segment %q", s)
	}
	seg := Segment{Type: m[1]}
	if m[2] != "" {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return Segment{}, fmt.Errorf("address: invalid index in segment %q: %w", s, err)
		}
		seg.Index = n
		seg.HasIndex = true
	}
	return seg, nil
}

// Parsed is the decomposition of a canonical content URL.
type Parsed struct {
	Scheme   string
	Platform string
	Thread   string
	Msg      string
	Segments []Segment
	Anchor   string
}

// New builds a canonical content URL from its parts. segs are the path
// segments in order (leaf last). anchor, when non-empty, is appended after '#'.
func New(scheme, platform, thread, msg string, segs []Segment, anchor string) (string, error) {
	if !schemeRe.MatchString(scheme) {
		return "", fmt.Errorf("address: invalid scheme %q", scheme)
	}
	for _, comp := range []string{platform, thread, msg} {
		if !compRe.MatchString(comp) {
			return "", fmt.Errorf("address: invalid URL component %q", comp)
		}
	}
	if anchor != "" && !anchorRe.MatchString(anchor) {
		return "", fmt.Errorf("address: invalid anchor %q", anchor)
	}
	var b strings.Builder
	b.WriteString(scheme)
	b.WriteString("://")
	b.WriteString(platform)
	b.WriteString("/")
	b.WriteString(thread)
	b.WriteString("/")
	b.WriteString(msg)
	for _, s := range segs {
		if !segRe.MatchString(s.String()) {
			return "", fmt.Errorf("address: invalid segment %q", s.String())
		}
		b.WriteString("/")
		b.WriteString(s.String())
	}
	if anchor != "" {
		b.WriteString("#")
		b.WriteString(anchor)
	}
	return b.String(), nil
}

// Parse decomposes a canonical content URL into its parts.
func Parse(raw string) (Parsed, error) {
	var p Parsed
	idx := strings.Index(raw, "://")
	if idx < 0 {
		return p, fmt.Errorf("address: missing scheme separator in %q", raw)
	}
	p.Scheme = raw[:idx]
	rest := raw[idx+3:]
	if !schemeRe.MatchString(p.Scheme) {
		return p, fmt.Errorf("address: invalid scheme %q", p.Scheme)
	}

	if i := strings.IndexByte(rest, '#'); i >= 0 {
		p.Anchor = rest[i+1:]
		rest = rest[:i]
		if !anchorRe.MatchString(p.Anchor) {
			return p, fmt.Errorf("address: invalid anchor %q", p.Anchor)
		}
	}
	if rest == "" {
		return p, fmt.Errorf("address: empty path in %q", raw)
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 3 {
		return p, fmt.Errorf("address: expected platform/thread/msg in %q", raw)
	}
	p.Platform, p.Thread, p.Msg = parts[0], parts[1], parts[2]
	for _, c := range []string{p.Platform, p.Thread, p.Msg} {
		if !compRe.MatchString(c) {
			return p, fmt.Errorf("address: invalid component %q", c)
		}
	}
	for _, s := range parts[3:] {
		seg, err := ParseSegment(s)
		if err != nil {
			return p, err
		}
		p.Segments = append(p.Segments, seg)
	}
	return p, nil
}

// NodeID returns the first 16 bytes of sha256(url) as 32 hex characters.
func NodeID(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:16])
}

// ContentID returns the full sha256 of body as 64 hex characters, used as the
// separate content-integrity identifier.
func ContentID(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
