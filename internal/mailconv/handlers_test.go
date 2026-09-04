package mailconv

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestRoute(t *testing.T) {
	cases := []struct {
		mime, ext string
		want      HandlerFunc
	}{
		{"application/pdf", ".pdf", pdfHandler},
		{"image/png", ".png", imageHandler},
		{"image/jpeg", ".jpg", imageHandler},
		{"text/plain", ".txt", textHandler},
		{"text/calendar", ".ics", icalHandler},
		{"text/html", ".html", htmlHandler},
		{"application/json", ".json", structuredHandler},
		{"application/xml", ".xml", structuredHandler},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx", officeHandler},
		{"application/msword", ".doc", legacyOfficeHandler},
		{"application/octet-stream", ".eml", emlHandler},
		{"application/octet-stream", ".bin", metadataHandler},
		{"video/mp4", ".mp4", metadataHandler},
		{"application/pdf", ".txt", pdfHandler}, // MIME is authoritative over ext
	}
	for _, c := range cases {
		got := Route(c.mime, c.ext)
		if name(got) != name(c.want) {
			t.Errorf("Route(%q, %q) = %s, want %s", c.mime, c.ext, name(got), name(c.want))
		}
	}
}

func name(f HandlerFunc) string {
	if f == nil {
		return "<nil>"
	}
	v := reflect.ValueOf(f)
	if v.IsNil() {
		return "<nil>"
	}
	return runtime.FuncForPC(v.Pointer()).Name()
}

func TestConvertAttachmentText(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(p, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ConvertAttachment(p, "note.txt", "text/plain", false); got != "hello world\n" {
		t.Errorf("text handler = %q", got)
	}
}

func TestConvertAttachmentImageSkipWithoutOCR(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	os.WriteFile(p, []byte("\x89PNG\r\n\x1a\n"), 0o644)
	if got := ConvertAttachment(p, "a.png", "image/png", false); !strings.Contains(got, "image skipped") {
		t.Errorf("image skip = %q", got)
	}
}

func TestConvertAttachmentMetadata(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "weird.bin")
	os.WriteFile(p, []byte("xyz"), 0o644)
	got := ConvertAttachment(p, "weird.bin", "application/octet-stream", false)
	if !strings.Contains(got, "attachment: weird.bin") {
		t.Errorf("metadata = %q", got)
	}
}

func TestConvertAttachmentErrorIsComment(t *testing.T) {
	// A valid .ics that is not really iCal must degrade to a comment, not fail.
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.ics")
	os.WriteFile(p, []byte("BEGIN:VCALENDAR\nbroken"), 0o644)
	got := ConvertAttachment(p, "bad.ics", "text/calendar", false)
	if !strings.Contains(got, "<!-- bad.ics:") {
		t.Errorf("ics error comment = %q", got)
	}
}
