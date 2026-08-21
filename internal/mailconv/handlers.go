// Type-handler registry for mail attachments, keyed by MIME type + extension.
//
// Routing is data-driven: MIME is authoritative, extension is a fallback tiebreak.
// A handler failure never fails the import; the error is emitted as an HTML
// comment so the leaf stays searchable.
package mailconv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ics "github.com/arran4/golang-ical"
	"github.com/jhillyerd/enmime/v2"

	"github.com/eSlider/2dph/internal/ocr"
)

// HandlerFunc converts one attachment to markdown. path is the on-disk file,
// name its display name, mime the authoritative MIME type.
type HandlerFunc func(path, name, mime string, doOCR bool) (string, error)

// handlerRoute binds a (MIME, extension) pair to a handler. mime ending in "/*"
// matches any subtype; an empty ext matches any extension.
type handlerRoute struct {
	mime string
	ext  string
	fn   HandlerFunc
}

// registry is evaluated in order, so more specific routes must come first.
// A mime ending in "*" is a prefix match; "image/*" is the common wildcard form.
var registry = []handlerRoute{
	{"application/pdf", "", pdfHandler},
	{"image/*", "", imageHandler},
	{"text/calendar", "", icalHandler},
	{"text/html", "", htmlHandler},
	{"text/*", "", textHandler},
	{"application/json", "", structuredHandler},
	{"application/xml", "", structuredHandler},
	{"application/xhtml*", "", htmlHandler},
	{"application/vnd.openxmlformats-*", "", officeHandler},
	{"application/msword", "", legacyOfficeHandler},
	{"application/vnd.ms-*", "", legacyOfficeHandler},
	{"application/octet-stream", ".eml", emlHandler},
}

// Route returns the first handler matching mime and ext, or the metadata
// handler for unknown types.
func Route(mime, ext string) HandlerFunc {
	mime = strings.ToLower(strings.TrimSpace(mime))
	ext = strings.ToLower(strings.TrimSpace(ext))
	for _, r := range registry {
		if !mimeMatch(r.mime, mime) {
			continue
		}
		if r.ext != "" && r.ext != ext {
			continue
		}
		return r.fn
	}
	return metadataHandler
}

// mimeMatch matches a wildcard pattern ("*" suffix) or an exact string.
func mimeMatch(pattern, mime string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(mime, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == mime
}

// ConvertAttachment routes one file through the registry, wrapping handler
// errors as HTML comments so import never aborts.
func ConvertAttachment(path, name, mime string, doOCR bool) string {
	fn := Route(mime, filepath.Ext(name))
	md, err := fn(path, name, mime, doOCR)
	if err != nil {
		return fmt.Sprintf("\n<!-- %s: %v -->\n", name, err)
	}
	return md
}

func pdfHandler(path, _, _ string, _ bool) (string, error) {
	return ocr.PDFFile(path)
}

func imageHandler(path, _, _ string, doOCR bool) (string, error) {
	if !doOCR {
		return "\n<!-- image skipped (pass --ocr) -->\n", nil
	}
	return ocr.ImageFile(path)
}

func textHandler(path, _, _ string, _ bool) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func htmlHandler(path, _, _ string, _ bool) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return StripHTML(string(raw)), nil
}

func emlHandler(path, _, _ string, _ bool) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	env, err := enmime.ReadEnvelope(f)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if env.Text != "" {
		b.WriteString(env.Text)
	} else if env.HTML != "" {
		b.WriteString(StripHTML(env.HTML))
	}
	return strings.TrimSpace(b.String()), nil
}

func structuredHandler(path, _, _ string, _ bool) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return "```\n" + string(raw) + "\n```", nil
}

// icalHandler reduces a calendar to summary lines (DTSTART SUMMARY LOCATION).
func icalHandler(path, _, _ string, _ bool) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	cal, err := ics.ParseCalendar(f)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, comp := range cal.Components {
		ev, ok := comp.(*ics.VEvent)
		if !ok {
			continue
		}
		if p := ev.GetProperty(ics.ComponentPropertyDtStart); p != nil {
			fmt.Fprintf(&b, "- when: %s\n", p.Value)
		}
		if p := ev.GetProperty(ics.ComponentPropertySummary); p != nil {
			fmt.Fprintf(&b, "- what: %s\n", p.Value)
		}
		if p := ev.GetProperty(ics.ComponentPropertyLocation); p != nil {
			fmt.Fprintf(&b, "- where: %s\n", p.Value)
		}
	}
	if b.Len() == 0 {
		return "", nil
	}
	return strings.TrimSpace(b.String()), nil
}

// officeHandler is metadata-only for OOXML (docx/xlsx/pptx); v1 skips unzip.
func officeHandler(path, name, _ string, _ bool) (string, error) {
	return metadata(path, name)
}

// legacyOfficeHandler is metadata-only for .doc/.xls (v1).
func legacyOfficeHandler(path, name, _ string, _ bool) (string, error) {
	return metadata(path, name)
}

func metadataHandler(path, name, mime string, _ bool) (string, error) {
	_ = mime
	return metadata(path, name)
}

func metadata(path, name string) (string, error) {
	var size string
	if st, err := os.Stat(path); err == nil {
		size = fmt.Sprintf("%d bytes", st.Size())
	} else {
		size = "unknown size"
	}
	return fmt.Sprintf("<!-- attachment: %s (%s) -->", name, size), nil
}
