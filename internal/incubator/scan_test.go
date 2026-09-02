package incubator

import (
	"os"
	"path/filepath"
	"testing"
)

// mkTree builds a miniature Thunderbird-profile fixture mirroring the гдеgroup
// corpus layout (epic #250): flat numeric message dirs at the profile root
// (INBOX), an INBOX_sbd container holding both flat numeric dirs and named
// <Folder>_sbd subtrees, plus an attachments/ copy that must be skipped.
func mkTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, mid string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		eml := "From: a@example.com\nTo: b@example.com\nDate: Tue, 01 Sep 2026 10:00:00 +0200\n"
		if mid != "" {
			eml += "Message-Id: <" + mid + ">\n"
		}
		eml += "Subject: x\n\nbody\n"
		if err := os.WriteFile(p, []byte(eml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// root flat = INBOX messages
	write("0000001/0000001.eml", "root-1@example.com")
	write("0000002/0000002.eml", "root-2@example.com")
	// attachment copy inside a message dir — never a message itself
	write("0000002/attachments/leaf.eml", "leaf-attachment@example.com")
	// INBOX_sbd flat = INBOX messages too
	write("INBOX_sbd/0000003/0000003.eml", "inbox-flat-3@example.com")
	// named subtree: INBOX/<Folder>
	write("INBOX_sbd/Archives_sbd/0000004/0000004.eml", "arch-4@example.com")
	write("INBOX_sbd/Projekte_sbd/0000005/0000005.eml", "proj-5@example.com")
	write("INBOX_sbd/Projekte_sbd/Wesseling_Stadt_sbd/0000006/0000006.eml", "wes-6@example.com")
	// MUTF-7 folder name (umlaut)
	write("INBOX_sbd/Projekte_sbd/Landesbetrieb_Mobilit&AOQ-t_Rheinland-Pfalz_(LBM)_sbd/0000007/0000007.eml", "lbm-7@example.com")
	// an id-less message → body-hash fallback key
	write("INBOX_sbd/Infos_sbd/0000008/0000008.eml", "")
	return root
}

func TestScanWheregroupTree(t *testing.T) {
	root := mkTree(t)
	msgs, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// 8 messages; the attachments/leaf.eml copy is excluded.
	if len(msgs) != 8 {
		t.Fatalf("Scan found %d messages, want 8", len(msgs))
	}
	want := []struct {
		rel     string
		mailbox string
	}{
		{"0000001/0000001.eml", "INBOX"},
		{"0000002/0000002.eml", "INBOX"},
		{"INBOX_sbd/0000003/0000003.eml", "INBOX"},
		{"INBOX_sbd/Archives_sbd/0000004/0000004.eml", "INBOX/Archives"},
		{"INBOX_sbd/Infos_sbd/0000008/0000008.eml", "INBOX/Infos"},
		{"INBOX_sbd/Projekte_sbd/0000005/0000005.eml", "INBOX/Projekte"},
		{"INBOX_sbd/Projekte_sbd/Landesbetrieb_Mobilit&AOQ-t_Rheinland-Pfalz_(LBM)_sbd/0000007/0000007.eml",
			"INBOX/Projekte/Landesbetrieb_Mobilität_Rheinland-Pfalz_(LBM)"},
		{"INBOX_sbd/Projekte_sbd/Wesseling_Stadt_sbd/0000006/0000006.eml", "INBOX/Projekte/Wesseling_Stadt"},
	}
	for i, m := range msgs {
		if m.Rel != want[i].rel {
			t.Errorf("msg[%d].Rel = %q, want %q", i, m.Rel, want[i].rel)
		}
		if m.Mailbox != want[i].mailbox {
			t.Errorf("msg[%d] %s: Mailbox = %q, want %q", i, m.Rel, m.Mailbox, want[i].mailbox)
		}
	}
	// Keys: Message-IDs canonicalized; the id-less message got a body hash.
	if got := msgs[0].Key; got != "root-1@example.com" {
		t.Errorf("msg[0].Key = %q, want root-1@example.com", got)
	}
	if last := msgs[4]; last.HasID || len(last.Key) != len("body-sha256:")+64 {
		t.Errorf("msg[4] fallback key wrong: HasID=%v Key=%q", last.HasID, last.Key)
	}
	// Deterministic order: path-ascending, root flat before INBOX_sbd.
	for i := 1; i < len(msgs); i++ {
		if msgs[i-1].Rel >= msgs[i].Rel {
			t.Fatalf("messages not sorted: %q >= %q", msgs[i-1].Rel, msgs[i].Rel)
		}
	}
}

func TestMailboxOfDir(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"", "INBOX"},
		{"0000001", "INBOX"},
		{"INBOX_sbd/0001914", "INBOX"},
		{"INBOX_sbd/Archives_sbd/0001912", "INBOX/Archives"},
		{"INBOX_sbd/Projekte_sbd/0002303", "INBOX/Projekte"},
		{"INBOX_sbd/Projekte_sbd/Wesseling_Stadt_sbd/0003410", "INBOX/Projekte/Wesseling_Stadt"},
		{"INBOX_sbd/Infos_sbd/Meldungen_sbd/0002297", "INBOX/Infos/Meldungen"},
	}
	for _, tc := range tests {
		if got := MailboxOfDir(tc.dir); got != tc.want {
			t.Errorf("MailboxOfDir(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}
