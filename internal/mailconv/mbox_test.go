package mailconv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mboxFixture is a synthetic two-message mbox (Alice/Bob, no real PII).
const mboxFixture = `From bob@example.com Tue Aug 18 09:00:00 2026
From: "Bob Builder" <bob@example.com>
To: alice@example.com
Subject: meeting notes
Date: Tue, 18 Aug 2026 09:00:00 +0000
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

Hi Alice, here are the notes.
From alice@example.com Wed Aug 19 10:30:00 2026
From: "Alice" <alice@example.com>
To: bob@example.com
Subject: Re: meeting notes
Date: Wed, 19 Aug 2026 10:30:00 +0000
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

Got them, thanks.
`

// TestSplitMbox proves the mbox splitter separates messages on real "From "
// separators (with sender+ctime envelope) and never splits on body lines that
// merely start with "From ".
func TestSplitMbox(t *testing.T) {
	msgs, err := SplitMbox(strings.NewReader(mboxFixture))
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("SplitMbox = %d messages, want 2", len(msgs))
	}
	if !strings.Contains(string(msgs[0]), "Subject: meeting notes") {
		t.Errorf("msg[0] missing subject; got:\n%s", msgs[0])
	}
	if !strings.Contains(string(msgs[1]), "Subject: Re: meeting notes") {
		t.Errorf("msg[1] missing subject; got:\n%s", msgs[1])
	}
	// A body line "From " must not split a message into more than expected.
	if !strings.Contains(string(msgs[0]), "Hi Alice") {
		t.Errorf("msg[0] missing body; got:\n%s", msgs[0])
	}
}

// TestMboxMessage proves mbox → mailconv.Message via the shared parseEML path,
// with the caller-supplied source-tag and folder preserved (issue #79).
func TestMboxMessage(t *testing.T) {
	msgs, err := SplitMbox(strings.NewReader(mboxFixture))
	if err != nil {
		t.Fatal(err)
	}
	got, err := MboxMessage(msgs[0], "tb-backup/archive", "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "tb-backup/archive" {
		t.Errorf("Source = %q, want tb-backup/archive", got.Source)
	}
	if got.Folder != "Inbox" {
		t.Errorf("Folder = %q, want Inbox", got.Folder)
	}
	if got.Subject != "meeting notes" {
		t.Errorf("Subject = %q, want %q", got.Subject, "meeting notes")
	}
	if !strings.Contains(got.TextBody, "Hi Alice") {
		t.Errorf("TextBody = %q, want to contain the body", got.TextBody)
	}
	if got.From != `"Bob Builder" <bob@example.com>` {
		t.Errorf("From = %q", got.From)
	}
}

// TestSkipFolderPolicy proves the #79 exclusion policy covers both the
// English Thunderbird names and the German Outlook folder names readpst emits
// (Entwürfe=Drafts, Vorlagen=Templates, Gelöschte Objekte=Deleted Items,
// Junk-E-Mail, Postausgang=Outbox/Unsent). Own folders (Inbox, Sent,
// Persönliche Ordner) stay importable.
func TestSkipFolderPolicy(t *testing.T) {
	excluded := []string{
		"Drafts", "Draft.sbd", "Templates", "Trash", "Junk", "Junk-E-Mail",
		"Spam", "Unsent Messages", "Entwürfe", "Vorlagen",
		"Gelöschte Objekte", "Postausgang",
	}
	for _, f := range excluded {
		if !SkipFolder(f) {
			t.Errorf("SkipFolder(%q) = false, want true (policy #79)", f)
		}
	}
	kept := []string{"Inbox", "Posteingang", "Gesendete Objekte", "Sent", "Persönliche Ordner", "Kalender"}
	for _, f := range kept {
		if SkipFolder(f) {
			t.Errorf("SkipFolder(%q) = true, want false", f)
		}
	}
}

// TestSplitMboxDirIdempotent proves the disk importer is idempotent across
// re-runs (state-free dedup by content-addressed output): a second run writes
// zero new messages and never duplicates the .eml tree (issue #79, like #74).
func TestSplitMboxDirIdempotent(t *testing.T) {
	root := t.TempDir()
	mb := filepath.Join(root, "tb-backup", "user@example.com", "Inbox")
	if err := os.MkdirAll(mb, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mb, "Inbox"), []byte(mboxFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "corpus", "mail")
	written, skipped, err := SplitMboxDir(root, out, "tb-backup", false)
	if err != nil {
		t.Fatal(err)
	}
	if written != 2 || skipped != 0 {
		t.Fatalf("first run: written=%d skipped=%d, want 2/0", written, skipped)
	}
	first := countEMLs(out)
	if first != 2 {
		t.Fatalf("first run produced %d .eml, want 2", first)
	}
	written, skipped, err = SplitMboxDir(root, out, "tb-backup", false)
	if err != nil {
		t.Fatal(err)
	}
	if written != 0 || skipped != 2 {
		t.Fatalf("re-run: written=%d skipped=%d, want 0/2 (no dupes)", written, skipped)
	}
	if second := countEMLs(out); second != first {
		t.Fatalf("re-run grew .eml tree: %d → %d", first, second)
	}
}

// TestFromEMLFlatRoot proves the contacts-eml layout (<root>/*.eml, flat) is
// ingested with the immediate dir as folder — the root added for issue #79.
func TestFromEMLFlatRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "contacts-eml")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	eml := `From: "Alice" <alice@example.com>
To: bob@example.com
Subject: flat root mail
Date: Thu, 20 Aug 2026 12:00:00 +0000
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

flat body
`
	if err := os.WriteFile(filepath.Join(root, "alice.eml"), []byte(eml), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, _, fail, err := FromEML(root, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok != 1 || fail != 0 {
		t.Fatalf("ok=%d fail=%d, want 1/0", ok, fail)
	}
	raw, err := os.ReadFile(filepath.Join(root, "message.json"))
	if err != nil {
		t.Fatal(err)
	}
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal message.json: %v", err)
	}
	if msg.Folder != "contacts-eml" {
		t.Fatalf("Folder = %q, want %q", msg.Folder, "contacts-eml")
	}
	if msg.Subject != "flat root mail" {
		t.Fatalf("Subject = %q", msg.Subject)
	}
}

func countEMLs(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, _ error) error {
		if !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ".eml") {
			n++
		}
		return nil
	})
	return n
}
