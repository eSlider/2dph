package mailsync

import (
	"context"
	"errors"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
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

// fakeIMAPClient mocks the connection seam (imapClient) so ListIDs retry is
// testable offline: the first N SEARCH attempts fail, later ones succeed.
type fakeIMAPClient struct {
	searches   int
	failFirst  int // number of leading SEARCH attempts that fail (transient)
	closeCalls int
	uids       imap.UIDSet
}

func (f *fakeIMAPClient) Close() error {
	f.closeCalls++
	return nil
}

func (f *fakeIMAPClient) Fetch(imap.NumSet, *imap.FetchOptions) *imapclient.FetchCommand {
	return nil
}

func (f *fakeIMAPClient) SearchAll() (*imap.SearchData, error) {
	f.searches++
	if f.searches <= f.failFirst {
		return nil, errors.New("transient search failure")
	}
	return &imap.SearchData{All: f.uids}, nil
}

// TestListIDsRetryAfterTransientError proves a transient SEARCH error triggers
// reconnect-and-redial: the dead connection is dropped (Close) and a second
// SEARCH succeeds, so the folder is not failed wholesale.
func TestListIDsRetryAfterTransientError(t *testing.T) {
	var uids imap.UIDSet
	uids.AddNum(7, 42)
	fake := &fakeIMAPClient{failFirst: 1, uids: uids}
	src := &imapSource{
		cfg:     IMAPConfig{Host: "mail.example.com", User: "alice@example.com", Password: "x"},
		mailbox: "INBOX",
		dir:     "INBOX",
		dialFn:  func() (imapClient, error) { return fake, nil },
	}

	ids, _, err := src.ListIDs(context.Background(), 0, "")
	if err != nil {
		t.Fatalf("ListIDs after retry: %v", err)
	}
	if fake.searches != 2 {
		t.Fatalf("expected 2 SEARCH attempts (initial + retry), got %d", fake.searches)
	}
	if fake.closeCalls < 1 {
		t.Fatalf("expected reconnect to Close the dead connection, got %d Close calls", fake.closeCalls)
	}
	if len(ids) != 2 || ids[0] != "7" || ids[1] != "42" {
		t.Fatalf("unexpected ids: %v", ids)
	}
}

// TestListIDsPropagatesFinalError proves the retry happens exactly once: when
// the redial attempt also fails, the final error is propagated (no busy loop).
func TestListIDsPropagatesFinalError(t *testing.T) {
	var uids imap.UIDSet
	uids.AddNum(9)
	fake := &fakeIMAPClient{failFirst: 2, uids: uids}
	src := &imapSource{
		cfg:     IMAPConfig{Host: "mail.example.com", User: "alice@example.com", Password: "x"},
		mailbox: "INBOX",
		dir:     "INBOX",
		dialFn:  func() (imapClient, error) { return fake, nil },
	}

	_, _, err := src.ListIDs(context.Background(), 0, "")
	if err == nil {
		t.Fatal("expected an error to propagate when the redial attempt also fails")
	}
	if fake.searches != 2 {
		t.Fatalf("expected exactly 2 SEARCH attempts (no busy loop), got %d", fake.searches)
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
