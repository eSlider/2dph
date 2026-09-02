package incubator

import "testing"

func TestDecodeUTF7(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Archives", "Archives"}, // no & — identity
		// Real corpus folder: Landesbetrieb_Mobilit&AOQ-t_Rheinland-Pfalz_(LBM)
		{"Landesbetrieb_Mobilit&AOQ-t_Rheinland-Pfalz_(LBM)",
			"Landesbetrieb_Mobilität_Rheinland-Pfalz_(LBM)"}, // &AOQ- = ä
		{"Vattenfall_GmbH", "Vattenfall_GmbH"},
		{"A&-B", "A&B"},            // &- = literal ampersand
		{"&ZeVnLIqe-", "日本語"},      // single-run CJK (65E5 672C 8A9E)
		{"&ZeU-&Zyw-&ip4-", "日本語"}, // split per-unit runs decode too
		{"&2D3eAA-", "😀"},          // surrogate pair (D83D DE00)
		{"&", "&"},                 // stray & without terminator → literal
		{"A&B", "A&B"},             // unterminated run → literal
	}
	for _, tc := range tests {
		if got := DecodeUTF7(tc.in); got != tc.want {
			t.Errorf("DecodeUTF7(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
