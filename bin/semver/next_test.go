package main

import "testing"

func TestNextForLine(t *testing.T) {
	tests := []struct {
		name string
		tags []string
		line string
		want string
		err  bool
	}{
		{
			name: "no tags in line starts at zero",
			tags: nil,
			line: "0.24",
			want: "v0.24.0",
		},
		{
			name: "other lines are ignored",
			tags: []string{"v0.23.9", "v0.25.0", "v1.0.4"},
			line: "0.24",
			want: "v0.24.0",
		},
		{
			name: "numeric tail wins over lexical order",
			tags: []string{"v0.24.9", "v0.24.10", "v0.24.2"},
			line: "0.24",
			want: "v0.24.11",
		},
		{
			name: "single tag",
			tags: []string{"v0.24.13"},
			line: "0.24",
			want: "v0.24.14",
		},
		{
			name: "non numeric tail is skipped",
			tags: []string{"v0.24.7", "v0.24.rc1", "v0.24."},
			line: "0.24",
			want: "v0.24.8",
		},
		{
			name: "two-digit major line",
			tags: []string{"v0.23.99", "v10.1.0"},
			line: "10.1",
			want: "v10.1.1",
		},
		{name: "line without minor", tags: nil, line: "0", err: true},
		{name: "line with patch", tags: nil, line: "0.24.1", err: true},
		{name: "empty line", tags: nil, line: "", err: true},
		{name: "leading v is rejected", tags: nil, line: "v0.24", err: true},
		{name: "trailing dot is rejected", tags: nil, line: "0.24.", err: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nextForLine(tc.tags, tc.line)
			if tc.err {
				if err == nil {
					t.Fatalf("nextForLine(%q) = %q, want error", tc.line, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("nextForLine(%q): %v", tc.line, err)
			}
			if got != tc.want {
				t.Fatalf("nextForLine(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}
