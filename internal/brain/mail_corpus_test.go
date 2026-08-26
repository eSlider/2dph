package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMailFixture creates one synthetic message (Alice/Bob, no PII) in the
// corpus layout <root>/<rel>/<id>/message.{md,json} — the same shape both the
// live corpus (var/corpus/mail/<folder>/<id>) and the legacy corpus
// (var/mail/<source>/<id>) use. msgBody is the markdown body under the
// "# subject" heading.
func writeMailFixture(t *testing.T, root, rel, id, subject, date, msgBody string) {
	t.Helper()
	dir := filepath.Join(root, rel, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := fmt.Sprintf("---\nid: %q\nfolder: %q\nsource: \"raw-email\"\nsubject: %q\nfrom: \"Alice <alice@example.com>\"\nto: \"Bob <bob@example.com>\"\ndate: %q\ntype: mail\n---\n\n# %s\n\n%s\n", id, rel, subject, date, subject, msgBody)
	if err := os.WriteFile(filepath.Join(dir, "message.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	j := fmt.Sprintf(`{"source":"raw-email","id":%q,"folder":%q,"subject":%q,"from":"Alice <alice@example.com>","to":"Bob <bob@example.com>","receivedAt":%q}`,
		id, rel, subject, date+"T10:00:00Z")
	if err := os.WriteFile(filepath.Join(dir, "message.json"), []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestLoadMailLeafsLegacyAndLiveRoots drives the acceptance case: the legacy
// var/mail structure (issue #184) and the live var/corpus/mail corpus must
// both be indexed by LoadMailLeafs (issue #199).
func TestLoadMailLeafsLegacyAndLiveRoots(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/gmail", "aaaaaaaaaaaaaaaa", "Legacy greetings", "2020-01-02", "hello from the legacy corpus")
	writeMailFixture(t, root, "var/corpus/mail/inbox", "1001", "Live greeting", "2026-08-01", "hello from the live inbox")

	leafs, err := LoadMailLeafs(MailRoots(root), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(leafs) != 2 {
		t.Fatalf("got %d leafs, want 2 (one per root)", len(leafs))
	}
	sources := map[string]bool{}
	texts := map[string]bool{}
	for _, lf := range leafs {
		if lf.Repo != "ooMail" || lf.How != "mail/import" || lf.Type != "mail" {
			t.Errorf("leaf metadata = %+v, want repo=ooMail how=mail/import type=mail", lf)
		}
		sources[lf.Source] = true
		texts[lf.Heading] = true
	}
	if !sources["ooMail:aaaaaaaaaaaaaaaa:message.md"] {
		t.Errorf("legacy root leaf missing, sources=%v", sources)
	}
	if !sources["ooMail:1001:message.md"] {
		t.Errorf("live root leaf missing, sources=%v", sources)
	}
	if !texts["Legacy greetings"] || !texts["Live greeting"] {
		t.Errorf("leaf headings = %v, want both subjects", texts)
	}
}

// TestLoadMailLeafsDedupAcrossRoots pins the sha256-content-address dedup:
// the same message (same content-address id, same rendered markdown) present
// in the legacy and the live corpus yields exactly one leaf, and a repeated
// load stays stable (idempotent).
func TestLoadMailLeafsDedupAcrossRoots(t *testing.T) {
	root := t.TempDir()
	// Same message, same content-address id, different corpus roots.
	writeMailFixture(t, root, "var/mail/archive/tb-backup-128g", "bbbbbbbbbbbbbbbb", "Re: project", "2021-03-04", "the body is identical")
	writeMailFixture(t, root, "var/corpus/mail/pst", "bbbbbbbbbbbbbbbb", "Re: project", "2021-03-04", "the body is identical")

	first, err := LoadMailLeafs(MailRoots(root), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("first load got %d leafs, want 1 (deduped by content address)", len(first))
	}
	second, err := LoadMailLeafs(MailRoots(root), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 {
		t.Fatalf("second load got %d leafs, want 1 (idempotent)", len(second))
	}
	if first[0].Source != "ooMail:bbbbbbbbbbbbbbbb:message.md" {
		t.Errorf("source = %q, want ooMail:bbbbbbbbbbbbbbbb:message.md", first[0].Source)
	}
}

// TestLoadMailLeafsSinceFilter keeps the --since behavior with several roots.
func TestLoadMailLeafsSinceFilter(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/gmail", "cccccccccccccccc", "Old message", "2019-06-01", "ancient")
	writeMailFixture(t, root, "var/corpus/mail/inbox", "2002", "New message", "2026-08-25", "fresh")

	leafs, err := LoadMailLeafs(MailRoots(root), "2026-01-01", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(leafs) != 1 {
		t.Fatalf("since filter got %d leafs, want 1", len(leafs))
	}
	if leafs[0].Heading != "New message" {
		t.Errorf("since filter kept %q, want the 2026 message", leafs[0].Heading)
	}
}

// TestLoadMailLeafsLimit applies the limit after dedup across roots.
func TestLoadMailLeafsLimit(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/gmail", "dddddddddddddddd", "One", "2020-01-01", "a")
	writeMailFixture(t, root, "var/mail/gmail", "eeeeeeeeeeeeeeee", "Two", "2020-01-02", "b")
	writeMailFixture(t, root, "var/mail/gmail", "ffffffffffffffff", "Three", "2020-01-03", "c")

	leafs, err := LoadMailLeafs(MailRoots(root), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(leafs) != 2 {
		t.Fatalf("limit got %d leafs, want 2", len(leafs))
	}
}

// TestLoadMailLeafsAttachmentMD checks attachment .md leafs are indexed with
// the message content address in the source.
func TestLoadMailLeafsAttachmentMD(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/inbox", "3001", "With attachment", "2026-08-01", "see attached")
	attDir := filepath.Join(root, "var", "mail", "inbox", "3001", "attachments")
	if err := os.MkdirAll(attDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attDir, "report.pdf.md"), []byte("# report\n\nthe report text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	leafs, err := LoadMailLeafs(MailRoots(root), "", 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, lf := range leafs {
		if strings.Contains(lf.Source, "report.pdf.md") {
			found = true
			if lf.Source != "ooMail:3001:report.pdf.md" {
				t.Errorf("attachment source = %q, want ooMail:3001:report.pdf.md", lf.Source)
			}
		}
	}
	if !found {
		t.Errorf("attachment leaf not indexed; got %d leafs", len(leafs))
	}
}
