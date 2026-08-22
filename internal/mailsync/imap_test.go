package mailsync

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestSanitizeFolder(t *testing.T) {
	cases := map[string]string{
		"INBOX":       "INBOX",
		"INBOX.Sent":  "INBOX.Sent", // dots stay readable
		"INBOX/Trash": "INBOX_Trash",
		"":            "_unnamed",
		"..":          "_unnamed",
		`a\b:c*d?"e<f>g|h i[j]`: `a_b_c_d__e_f_g_h_ij`,
	}
	for in, want := range cases {
		if got := SanitizeFolder(in); got != want {
			t.Errorf("SanitizeFolder(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIMAPEnv(t *testing.T) {
	env := map[string]string{
		"IMAP_HOST":     "web56.alfahosting-server.de",
		"IMAP_PORT":     "993",
		"IMAP_USER":     "info@medex-pflegedienst.de",
		"IMAP_PASSWORD": "x",
		"IMAP_FOLDERS":  "INBOX, INBOX.Sent ,",
	}
	cfg, err := IMAPEnv(env)
	if err != nil {
		t.Fatalf("IMAPEnv: %v", err)
	}
	if cfg.Port != 993 || len(cfg.Folders) != 2 || cfg.Folders[1] != "INBOX.Sent" {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	delete(env, "IMAP_FOLDERS")
	cfg, err = IMAPEnv(env)
	if err != nil || len(cfg.Folders) != 0 {
		t.Fatalf("empty folders should auto-list: %+v err=%v", cfg, err)
	}

	// MATCHA_IMAP_PASSWORD fallback (mails project keyring export convention).
	delete(env, "IMAP_PASSWORD")
	env["MATCHA_IMAP_PASSWORD"] = "y"
	if _, err := IMAPEnv(env); err != nil {
		t.Fatalf("MATCHA_IMAP_PASSWORD fallback failed: %v", err)
	}
}

func TestHasNoSelect(t *testing.T) {
	if !hasNoSelect([]imap.MailboxAttr{imap.MailboxAttrNoSelect}) {
		t.Fatal("\\Noselect not detected")
	}
	if hasNoSelect([]imap.MailboxAttr{imap.MailboxAttrHasChildren}) {
		t.Fatal("false positive")
	}
}
