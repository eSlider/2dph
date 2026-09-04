package mailconv

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture sets cover plain, multipart/alternative (text+html), multipart/mixed
// (attachment), nested alternative+mixed, and an iso-8859-1 quoted-printable
// body to prove charset decoding via x/text.
var fixtures = []string{"plain", "alternative", "mixed", "nested", "charset"}

func fixtureReader(t *testing.T, name string) io.Reader {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name+".eml"))
	if err != nil {
		t.Fatalf("open fixture %s: %v", name, err)
	}
	return f
}

func mustParse(t *testing.T, name string) (Message, []attachmentPart) {
	t.Helper()
	r := fixtureReader(t, name)
	t.Cleanup(func() { r.(io.Closer).Close() })
	res, err := parseEML(r)
	if err != nil {
		t.Fatalf("parseEML(%s): %v", name, err)
	}
	return res.msg, res.parts
}

func TestParsePlain(t *testing.T) {
	msg, parts := mustParse(t, "plain")
	if msg.TextBody != "Hello world.\nThis is a plain single-part message." {
		t.Errorf("TextBody = %q", msg.TextBody)
	}
	if msg.HTMLBody != "" {
		t.Errorf("HTMLBody = %q, want empty", msg.HTMLBody)
	}
	if len(parts) != 0 {
		t.Errorf("parts = %d, want 0", len(parts))
	}
	if msg.Subject != "Plain — contract review" {
		t.Errorf("Subject = %q (RFC2047 decode)", msg.Subject)
	}
	if msg.From != `"Pat Sample" <pat@example.com>` {
		t.Errorf("From = %q", msg.From)
	}
	if msg.Date.IsZero() {
		t.Error("Date not parsed")
	}
}

func TestParseAlternative(t *testing.T) {
	msg, parts := mustParse(t, "alternative")
	if msg.TextBody != "Please review the attached terms." {
		t.Errorf("TextBody = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<p>Please review the attached terms.</p>") {
		t.Errorf("HTMLBody = %q", msg.HTMLBody)
	}
	if len(parts) != 0 {
		t.Errorf("parts = %d, want 0", len(parts))
	}
}

func TestParseMixed(t *testing.T) {
	msg, parts := mustParse(t, "mixed")
	if msg.TextBody != "body text" {
		t.Errorf("TextBody = %q", msg.TextBody)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}
	if parts[0].FileName != "terms.txt" || parts[0].ContentType != "text/plain" ||
		strings.TrimSpace(string(parts[0].Content)) != "the terms" {
		t.Errorf("parts[0] = %+v", parts[0])
	}
	if parts[1].FileName != "blob.bin" || parts[1].ContentType != "application/octet-stream" {
		t.Errorf("parts[1] = %+v", parts[1])
	}
}

func TestParseNestedAlternativeMixed(t *testing.T) {
	msg, parts := mustParse(t, "nested")
	if msg.TextBody != "Nested text body." {
		t.Errorf("TextBody = %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<p>Nested <b>html</b> body.</p>") {
		t.Errorf("HTMLBody = %q", msg.HTMLBody)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d, want 2", len(parts))
	}
	if parts[0].FileName != "notes.txt" || strings.TrimSpace(string(parts[0].Content)) != "nested attachment data" {
		t.Errorf("parts[0] = %+v", parts[0])
	}
	if parts[1].FileName != "logo.png" || parts[1].ContentType != "image/png" {
		t.Errorf("parts[1] = %+v", parts[1])
	}
}

func TestParseCharsetLatin1(t *testing.T) {
	msg, parts := mustParse(t, "charset")
	if !strings.Contains(msg.TextBody, "Café is at Müllerstraße 12. Grüße!") {
		t.Errorf("TextBody = %q (want charset-decoded UTF-8)", msg.TextBody)
	}
	if msg.Subject != "Café meeting" {
		t.Errorf("Subject = %q (want RFC2047+charset decode)", msg.Subject)
	}
	if len(parts) != 0 {
		t.Errorf("parts = %d, want 0", len(parts))
	}
}

func BenchmarkParseEML(b *testing.B) {
	for _, name := range fixtures {
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				f, err := os.Open(filepath.Join("testdata", name+".eml"))
				if err != nil {
					b.Fatal(err)
				}
				if _, err := parseEML(f); err != nil {
					b.Fatal(err)
				}
				f.Close()
			}
		})
	}
}
