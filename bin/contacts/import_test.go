package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vcard21 = "BEGIN:VCARD\r\nVERSION:2.1\r\nN:;Alex\r\nFN:Alex\r\nTEL;HOME:016098349975\r\nEMAIL:alex@example.com\r\nEND:VCARD\r\n"

const vcard30 = `BEGIN:VCARD
VERSION:3.0
N:Gestiona(RO);*1215;;;
FN:*1215 Gestiona(RO)
TEL;TYPE=CELL;TYPE=PREF:1215
EMAIL;TYPE=WORK:*1215@ro.example
ORG:Company X
TITLE:Manager
END:VCARD
`

const csvRow = "Name,Given Name,Family Name,Photo,E-mail 1 - Value,Phone 1 - Value,Organization 1 - Name,Organization 1 - Title\n" +
	"Jane Doe,Jane,Doe,https://p/j.png,jane@x.io,+49 176 123,ACME,Engineer\n" +
	"Bob,Bob,Builder,,bob@x.io,+34 600 1,,,,\n"

func writeFixture(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseVCard21(t *testing.T) {
	p := writeFixture(t, "a.vcf", vcard21)
	cs, err := parseVCardFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("want 1 contact, got %d", len(cs))
	}
	c := cs[0]
	if c.FullName != "Alex" {
		t.Errorf("FullName = %q", c.FullName)
	}
	if c.Given != "Alex" {
		t.Errorf("Given = %q", c.Given)
	}
	if len(c.Emails) != 1 || c.Emails[0] != "alex@example.com" {
		t.Errorf("Emails = %v", c.Emails)
	}
	if len(c.Phones) != 1 || c.Phones[0] != "016098349975" {
		t.Errorf("Phones = %v", c.Phones)
	}
}

func TestParseVCard30(t *testing.T) {
	p := writeFixture(t, "b.vcf", vcard30)
	cs, err := parseVCardFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("want 1 contact, got %d", len(cs))
	}
	c := cs[0]
	if c.FullName != "*1215 Gestiona(RO)" {
		t.Errorf("FullName = %q", c.FullName)
	}
	if c.Org != "Company X" {
		t.Errorf("Org = %q", c.Org)
	}
	if c.Title != "Manager" {
		t.Errorf("Title = %q", c.Title)
	}
	if len(c.Emails) != 1 || c.Emails[0] != "*1215@ro.example" {
		t.Errorf("Emails = %v", c.Emails)
	}
}

func TestParseGoogleCSV(t *testing.T) {
	p := writeFixture(t, "c.csv", csvRow)
	cs, err := parseGoogleCSV(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("want 2 contacts, got %d", len(cs))
	}
	jane := cs[0]
	if jane.FullName != "Jane Doe" || jane.Given != "Jane" || jane.Family != "Doe" {
		t.Errorf("name fields = %q/%q/%q", jane.FullName, jane.Given, jane.Family)
	}
	if len(jane.Emails) != 1 || jane.Emails[0] != "jane@x.io" {
		t.Errorf("Jane Emails = %v", jane.Emails)
	}
	if len(jane.Phones) != 1 || jane.Phones[0] != "+49 176 123" {
		t.Errorf("Jane Phones = %v", jane.Phones)
	}
	if jane.Org != "ACME" || jane.Title != "Engineer" {
		t.Errorf("Jane org/title = %q/%q", jane.Org, jane.Title)
	}
	if jane.Photo != "https://p/j.png" {
		t.Errorf("Jane Photo = %q", jane.Photo)
	}
	// Bob row is short; parser must tolerate ragged rows (FieldsPerRecord=-1).
	bob := cs[1]
	if bob.FullName != "Bob" {
		t.Errorf("Bob FullName = %q", bob.FullName)
	}
}

func TestParseMAB(t *testing.T) {
	mab := `// <(a=c)>
< <(a=c)> (83=FirstName)(84=LastName)(87=DisplayName)(89=PrimaryEmail)(8F=WorkPhone)
  < <(a=c+1)>
   (83=Martina)(84=Jaenisch)(87=Martina Jaenisch)(89=martina.jaenisch@wheregroup.com)(8F=+49 30 1)
  >
>
`
	p := writeFixture(t, "d.mab", mab)
	cs, err := parseMAB(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("want 1 contact, got %d", len(cs))
	}
	c := cs[0]
	if c.FullName != "Martina Jaenisch" {
		t.Errorf("FullName = %q", c.FullName)
	}
	if c.Given != "Martina" || c.Family != "Jaenisch" {
		t.Errorf("given/family = %q/%q", c.Given, c.Family)
	}
	if len(c.Emails) != 1 || c.Emails[0] != "martina.jaenisch@wheregroup.com" {
		t.Errorf("Emails = %v", c.Emails)
	}
	if len(c.Phones) != 1 || c.Phones[0] != "+49 30 1" {
		t.Errorf("Phones = %v", c.Phones)
	}
}

func TestLoadSourcesDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "x.vcf"), []byte(vcard30), 0o644)
	sub := filepath.Join(dir, "sub")
	_ = os.Mkdir(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "y.vcf"), []byte(vcard21), 0o644)
	cs, err := loadSources([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("want 2 contacts, got %d", len(cs))
	}
}

func TestMarkdown(t *testing.T) {
	c := Contact{FullName: "Jane Doe", Emails: []string{"jane@x.io"}, Phones: []string{"+49 1"}}
	s := c.Markdown()
	if !strings.Contains(s, "Jane Doe") || !strings.Contains(s, "jane@x.io") || !strings.Contains(s, "+49 1") {
		t.Errorf("markdown = %q", s)
	}
}

func TestMorkUnquote(t *testing.T) {
	if got := morkUnquote(`martina^40jaenisch`); got != "martina@jaenisch" {
		t.Errorf("morkUnquote = %q", got)
	}
}
