package address

import "testing"

func TestSegmentParseRender(t *testing.T) {
	cases := []struct {
		in       string
		typ      string
		idx      int
		hasIndex bool
		render   string
		wantErr  bool
	}{
		{in: "p[3]", typ: "p", idx: 3, hasIndex: true},
		{in: "table[0]", typ: "table", idx: 0, hasIndex: true},
		{in: "body", typ: "body", hasIndex: false},
		{in: "tr[1]", typ: "tr", idx: 1, hasIndex: true},
		{in: "", wantErr: true},
		{in: "3", wantErr: true},
		{in: "p[]", wantErr: true},
		{in: "p[-1]", wantErr: true},
		{in: "p[a]", wantErr: true},
		{in: "p[3]x", wantErr: true},
		{in: "2p", wantErr: true},
		{in: "p[007]", typ: "p", idx: 7, hasIndex: true, render: "p[7]"},
	}
	for _, c := range cases {
		seg, err := ParseSegment(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseSegment(%q): expected error, got %+v", c.in, seg)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSegment(%q): unexpected error: %v", c.in, err)
			continue
		}
		if seg.Type != c.typ || seg.Index != c.idx || seg.HasIndex != c.hasIndex {
			t.Errorf("ParseSegment(%q) = %+v, want typ=%q idx=%d hasIndex=%v", c.in, seg, c.typ, c.idx, c.hasIndex)
		}
		want := c.render
		if want == "" {
			want = c.in
		}
		if got := seg.String(); got != want {
			t.Errorf("Segment{%q,%d}.String() = %q, want %q", c.typ, c.idx, got, want)
		}
	}
}

func TestBuildParseRoundTrip(t *testing.T) {
	segs := []Segment{
		{Type: "body"},
		{Type: "p", Index: 3, HasIndex: true},
		{Type: "table", Index: 0, HasIndex: true},
	}
	raw, err := New("mail", "gmail", "T42", "M17", segs, "r2,c5")
	if err != nil {
		t.Fatal(err)
	}
	want := "mail://gmail/T42/M17/body/p[3]/table[0]#r2,c5"
	if raw != want {
		t.Fatalf("New = %q, want %q", raw, want)
	}

	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Scheme != "mail" || p.Platform != "gmail" || p.Thread != "T42" || p.Msg != "M17" {
		t.Fatalf("Parse header mismatch: %+v", p)
	}
	if p.Anchor != "r2,c5" {
		t.Errorf("anchor = %q, want r2,c5", p.Anchor)
	}
	if len(p.Segments) != 3 {
		t.Fatalf("segments = %+v", p.Segments)
	}
	for i, s := range p.Segments {
		if s.String() != segs[i].String() {
			t.Errorf("segment[%d] = %q, want %q", i, s.String(), segs[i].String())
		}
	}
}

func TestBuildNoSegmentsNoAnchor(t *testing.T) {
	raw, err := New("mail", "gmail", "T42", "M17", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if raw != "mail://gmail/T42/M17" {
		t.Fatalf("got %q, want mail://gmail/T42/M17", raw)
	}
	p, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Segments) != 0 || p.Anchor != "" {
		t.Fatalf("unexpected parts: %+v", p)
	}
}

func TestBuildValidation(t *testing.T) {
	bad := []struct{ scheme, platform, thread, msg string }{
		{"", "gmail", "T", "M"},
		{"ma il", "gmail", "T", "M"},
		{"mail", "", "T", "M"},
		{"mail", "gm/ail", "T", "M"},
		{"mail", "gmail", "T/1", "M"},
		{"mail", "gmail", "T", "M#x"},
	}
	for _, c := range bad {
		if _, err := New(c.scheme, c.platform, c.thread, c.msg, nil, ""); err == nil {
			t.Errorf("New(%q,%q,%q,%q): expected error", c.scheme, c.platform, c.thread, c.msg)
		}
	}
	// segment validation
	if _, err := New("mail", "gmail", "T", "M", []Segment{{Type: "p a"}}, ""); err == nil {
		t.Error("expected error for bad segment type")
	}
	if _, err := New("mail", "gmail", "T", "M", nil, "x/y"); err == nil {
		t.Error("expected error for anchor with slash")
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"",
		"mail",
		"://gmail/T/M",
		"mail://",
		"mail://gmail",
		"mail://gmail/T",
		"mail://gmail/T/M/", // trailing slash → empty segment
		"mail://gmail/T/M/bad seg",
		"mail://gmail/T/M/p[3]#", // anchor parse ok but empty
	}
	for _, s := range bad {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): expected error", s)
		}
	}
}

func TestNodeID(t *testing.T) {
	u1, _ := New("mail", "gmail", "T42", "M17", []Segment{{Type: "p", Index: 3, HasIndex: true}}, "")
	u2, _ := New("mail", "gmail", "T42", "M17", []Segment{{Type: "p", Index: 4, HasIndex: true}}, "")
	if NodeID(u1) == NodeID(u2) {
		t.Error("different URLs must yield different node IDs")
	}
	if len(NodeID(u1)) != 32 {
		t.Errorf("node ID length = %d, want 32 hex (16 bytes)", len(NodeID(u1)))
	}
	if NodeID(u1) != NodeID(u1) {
		t.Error("deterministic expected")
	}
}

func TestContentID(t *testing.T) {
	a := ContentID("hello world")
	b := ContentID("hello world")
	c := ContentID("hello worlD")
	if a != b {
		t.Error("ContentID must be deterministic")
	}
	if a == c {
		t.Error("different bodies must differ")
	}
	if len(a) != 64 {
		t.Errorf("content ID length = %d, want 64 hex (full sha256)", len(a))
	}
}
