package utils

import "testing"

func TestSafeSegment(t *testing.T) {
	cases := []struct{ in, want string }{
		{"m1@example.com", "m1@example.com"},
		{"a/b\\c", "a-b-c"},
		{"../evil", "evil"},
		{"..", "-"},
		{"line1\ncontrol", "line1-control"},
		{"", "-"},
		{"m\x7f", "m"},
	}
	for _, c := range cases {
		if got := SafeSegment(c.in); got != c.want {
			t.Errorf("SafeSegment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
