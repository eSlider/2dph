// Package mailconv converts synced message.json trees to markdown (no Ladybug).
package mailconv

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/ocr"
)

type Attachment struct {
	FileID      string `json:"fileId,omitempty"`
	FileName    string `json:"fileName"`
	StoredName  string `json:"storedName"`
	Size        int64  `json:"size,omitempty"`
	ContentType string `json:"contentType,omitempty"`
}

type Message struct {
	Source         string       `json:"source"`
	ID             string       `json:"id"`
	Folder         string       `json:"folder"`
	Subject        string       `json:"subject,omitempty"`
	From           string       `json:"from,omitempty"`
	To             string       `json:"to,omitempty"`
	CC             string       `json:"cc,omitempty"`
	BCC            string       `json:"bcc,omitempty"`
	ReceivedAt     time.Time    `json:"receivedAt,omitempty"`
	HTMLBody       string       `json:"htmlBody,omitempty"`
	TextBody       string       `json:"textBody,omitempty"`
	HasAttachments bool         `json:"hasAttachments,omitempty"`
	Attachments    []Attachment `json:"attachments,omitempty"`
}

var (
	reScript = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reBR     = regexp.MustCompile(`(?i)<br\s*/?>`)
	reP      = regexp.MustCompile(`(?i)</p>`)
	reTag    = regexp.MustCompile(`(?s)<[^>]+>`)
)

// StripHTML is a fallback HTML→text converter (no markitdown).
func StripHTML(s string) string {
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reBR.ReplaceAllString(s, "\n")
	s = reP.ReplaceAllString(s, "\n\n")
	s = reTag.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

func BodyMarkdown(msg Message) string {
	if strings.TrimSpace(msg.TextBody) != "" {
		return strings.TrimSpace(msg.TextBody)
	}
	if strings.TrimSpace(msg.HTMLBody) != "" {
		return StripHTML(msg.HTMLBody)
	}
	return ""
}

func ConvertAttachment(path string, doOCR bool) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".markdown", ".txt", ".csv", ".json", ".xml", ".yaml", ".yml", ".log", ".tsv", ".ics", ".ical", ".vcf", ".eml":
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	case ".pdf":
		text, err := ocr.PDFFile(path)
		if err != nil {
			return fmt.Sprintf("\n<!-- pdf convert failed: %v -->\n", err), nil
		}
		return text, nil
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".tif", ".webp":
		if !doOCR {
			return "\n<!-- image skipped (pass --ocr) -->\n", nil
		}
		text, err := ocr.ImageFile(path)
		if err != nil {
			return fmt.Sprintf("\n<!-- ocr failed: %v -->\n", err), nil
		}
		return text, nil
	case ".html", ".htm":
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return StripHTML(string(raw)), nil
	default:
		return fmt.Sprintf("\n<!-- skipped %s -->\n", ext), nil
	}
}

// FromRaw converts all message.json under root to message.md (+ attachment .md).
func FromRaw(root string, ocrEnabled, force, dryRun bool) (ok, skip, fail int, err error) {
	err = filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		if filepath.Base(p) != "message.json" {
			return nil
		}
		msgDir := filepath.Dir(p)
		mdPath := filepath.Join(msgDir, "message.md")
		if !force {
			if st, err := os.Stat(mdPath); err == nil && st.Size() > 0 {
				skip++
				return nil
			}
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			fail++
			fmt.Fprintf(os.Stderr, "  [fail] %s: %v\n", msgDir, err)
			return nil
		}
		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			fail++
			fmt.Fprintf(os.Stderr, "  [fail] %s: %v\n", msgDir, err)
			return nil
		}
		body := BodyMarkdown(msg)
		date := ""
		if !msg.ReceivedAt.IsZero() {
			date = msg.ReceivedAt.UTC().Format("2006-01-02")
		}
		var b strings.Builder
		b.WriteString("---\n")
		fmt.Fprintf(&b, "id: %q\n", msg.ID)
		fmt.Fprintf(&b, "folder: %q\n", msg.Folder)
		fmt.Fprintf(&b, "source: %q\n", msg.Source)
		fmt.Fprintf(&b, "subject: %q\n", msg.Subject)
		fmt.Fprintf(&b, "from: %q\n", msg.From)
		fmt.Fprintf(&b, "to: %q\n", msg.To)
		fmt.Fprintf(&b, "date: %q\n", date)
		b.WriteString("type: mail\n")
		b.WriteString("---\n\n")
		fmt.Fprintf(&b, "# %s\n\n", or(msg.Subject, "(no subject)"))
		b.WriteString(body)
		b.WriteString("\n")

		if dryRun {
			ok++
			fmt.Printf("  [dry ] %s %s\n", or(date, "?"), truncate(msg.Subject, 60))
			return nil
		}
		if err := os.WriteFile(mdPath, []byte(b.String()), 0o644); err != nil {
			fail++
			return nil
		}
		attDir := filepath.Join(msgDir, "attachments")
		for _, a := range msg.Attachments {
			name := a.StoredName
			if name == "" {
				name = a.FileName
			}
			if name == "" {
				continue
			}
			src := filepath.Join(attDir, name)
			if _, err := os.Stat(src); err != nil {
				continue
			}
			text, err := ConvertAttachment(src, ocrEnabled)
			if err != nil || text == "" {
				continue
			}
			outName := a.FileName
			if outName == "" {
				outName = name
			}
			outName = outName + ".md"
			_ = os.WriteFile(filepath.Join(attDir, outName), []byte(text), 0o644)
		}
		ok++
		fmt.Printf("  [ok  ] %s %s\n", or(date, "?"), truncate(msg.Subject, 60))
		return nil
	})
	return ok, skip, fail, err
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
