package mailsync

import (
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestSanitizeFolder(t *testing.T) {
	cases := map[string]string{
		"INBOX":                 "INBOX",
		"INBOX.Sent":            "INBOX.Sent", // dots stay readable
		"INBOX/Trash":           "INBOX_Trash",
		"":                      "_unnamed",
		"..":                    "_unnamed",
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
		"IMAP_HOST":     "mail.example.com",
		"IMAP_PORT":     "993",
		"IMAP_USER":     "alice@example.com",
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

	// MAIL_IMAP_PASSWORD alias fallback, self-contained to the IMAP key set.
	delete(env, "IMAP_PASSWORD")
	env["MAIL_IMAP_PASSWORD"] = "y"
	if _, err := IMAPEnv(env); err != nil {
		t.Fatalf("MAIL_IMAP_PASSWORD fallback failed: %v", err)
	}
}

// TestParseIMAPMessage proves the emersion/go-message parser (finding 5)
// extracts headers + text/html bodies from a raw RFC 822 without enmime.
// Fixtures are synthetic (Alice/Bob/example.com).
func TestParseIMAPMessage(t *testing.T) {
	raw := []byte("From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@example.com>\r\n" +
		"Subject: Test message\r\n" +
		"Date: Mon, 05 Aug 2024 10:00:00 +0200\r\n" +
		"Message-ID: <imap-1@example.com>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=\"B\"\r\n" +
		"\r\n" +
		"--B\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"plain body\r\n" +
		"--B\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n" +
		"\r\n" +
		"<p>html body</p>\r\n" +
		"--B--\r\n")

	m, err := parseIMAPMessage("INBOX.Sent", "42", raw)
	if err != nil {
		t.Fatalf("parseIMAPMessage: %v", err)
	}
	if m.Folder != "INBOX.Sent" || m.ID != "42" {
		t.Fatalf("unexpected folder/id: %+v", m)
	}
	if m.Subject != "Test message" || m.From != "Alice <alice@example.com>" {
		t.Fatalf("headers wrong: %+v", m)
	}
	if m.TextBody != "plain body" || m.HTMLBody != "<p>html body</p>" {
		t.Fatalf("bodies wrong: text=%q html=%q", m.TextBody, m.HTMLBody)
	}
	if m.ReceivedAt.IsZero() {
		t.Fatal("date not parsed")
	}
	if m.MimeMessageID != "<imap-1@example.com>" {
		t.Fatalf("message-id wrong: %q", m.MimeMessageID)
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
