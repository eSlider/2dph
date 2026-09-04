package mailconv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const emlFixture = `From: "Pat Sample" <pat@example.com>
To: info@example.com
Subject: Re: contract review
Date: Tue, 19 Aug 2026 13:47:04 +0100
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="ALT"

--ALT
Content-Type: text/plain; charset=utf-8

Please review the attached terms.
--ALT
Content-Type: text/html; charset=utf-8

<p>Please review the attached terms.</p>
--ALT--
`

func TestFromEMLWritesDatedMessageMD(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inbox", "1234")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "msg.eml"), []byte(emlFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, skip, fail, err := FromEML(root, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok != 1 || skip != 0 || fail != 0 {
		t.Fatalf("ok=%d skip=%d fail=%d, want 1/0/0", ok, skip, fail)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "message.md"))
	if err != nil {
		t.Fatal(err)
	}
	md := string(raw)
	for _, want := range []string{
		"id: \"msg\"",
		"source: \"raw-email\"",
		"subject: \"Re: contract review\"",
		"date: \"2026-08-19\"",
		"type: mail",
		"# Re: contract review",
		"Please review the attached terms.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("message.md missing %q; got:\n%s", want, md)
		}
	}
}

// TestFromEMLFile converts a single .eml (per-blob entry used by the ETL
// runner #98) and is idempotent across repeated calls.
func TestFromEMLFileConvertsOneEML(t *testing.T) {
	dir := t.TempDir()
	eml := filepath.Join(dir, "msg.eml")
	if err := os.WriteFile(eml, []byte(emlFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := FromEMLFile(eml, false); err != nil {
		t.Fatalf("FromEMLFile: %v", err)
	}
	mdPath := filepath.Join(dir, "message.md")
	md, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "# Re: contract review") {
		t.Fatalf("message.md missing expected subject; got:\n%s", md)
	}
	// Idempotent: second call leaves the .md untouched.
	if err := FromEMLFile(eml, false); err != nil {
		t.Fatalf("FromEMLFile second call: %v", err)
	}
	md2, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(md2) != string(md) {
		t.Fatal("FromEMLFile second call rewrote an existing message.md (not idempotent)")
	}
}

func TestFromEMLAttachmentsRoutedByMIME(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inbox", "5")
	os.MkdirAll(dir, 0o755)
	eml := `From: a@example.com
To: b@example.com
Subject: with attachment
Date: Mon, 18 Aug 2026 09:00:00 +0000
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="MX"

--MX
Content-Type: text/plain

body text
--MX
Content-Type: text/plain; name="terms.txt"
Content-Disposition: attachment; filename="terms.txt"

the terms
--MX
Content-Type: application/octet-stream; name="blob.bin"
Content-Disposition: attachment; filename="blob.bin"

binary-data
--MX--
`
	if err := os.WriteFile(filepath.Join(dir, "m.eml"), []byte(eml), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, _, fail, err := FromEML(root, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok != 1 || fail != 0 {
		t.Fatalf("ok=%d fail=%d", ok, fail)
	}
	att := filepath.Join(dir, "attachments")
	if raw, err := os.ReadFile(filepath.Join(att, "terms.txt.md")); err != nil {
		t.Errorf("terms.txt.md missing: %v", err)
	} else if strings.TrimSpace(string(raw)) != "the terms" {
		t.Errorf("terms.txt.md = %q", string(raw))
	}
	raw, err := os.ReadFile(filepath.Join(att, "blob.bin.md"))
	if err != nil {
		t.Fatalf("blob.bin.md missing: %v", err)
	}
	if !strings.Contains(string(raw), "attachment: blob.bin") {
		t.Errorf("blob.bin.md = %q", string(raw))
	}
}

func TestFromEMLDryRunAndSkip(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "inbox", "9")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "m.eml"), []byte(emlFixture), 0o644)
	ok, _, _, err := FromEML(root, false, false, true)
	if err != nil || ok != 1 {
		t.Fatalf("dry-run ok=%d err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "message.md")); err == nil {
		t.Fatal("dry-run should not write message.md")
	}
	ok, _, _, err = FromEML(root, false, false, false)
	if err != nil || ok != 1 {
		t.Fatalf("write ok=%d err=%v", ok, err)
	}
	_, skip, _, err := FromEML(root, false, false, false)
	if err != nil || skip != 1 {
		t.Fatalf("skip=%d err=%v, want 1", skip, err)
	}
}

// TestFromEMLFolderNestedLayout proves that for the mailsync v1 layout
// <root>/<folder>/<id>/<id>.eml the extracted folder is the parent of the
// <id> dir (the mail folder), not the immediate <id> directory (#111).
func TestFromEMLFolderNestedLayout(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Sent Items", "abc123")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	eml := filepath.Join(dir, "abc123.eml")
	if err := os.WriteFile(eml, []byte(emlFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, _, fail, err := FromEML(root, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if ok != 1 || fail != 0 {
		t.Fatalf("ok=%d fail=%d, want 1/0", ok, fail)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "message.json"))
	if err != nil {
		t.Fatal(err)
	}
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("unmarshal message.json: %v", err)
	}
	if msg.Folder != "Sent Items" {
		t.Fatalf("folder = %q, want %q (layout %q)", msg.Folder, "Sent Items", eml)
	}
	md, err := os.ReadFile(filepath.Join(dir, "message.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "folder: \"Sent Items\"") {
		t.Errorf("message.md missing folder: %q; got:\n%s", "Sent Items", string(md))
	}
}
