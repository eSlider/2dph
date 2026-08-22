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

	"github.com/jhillyerd/enmime/v2"
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
	Date           time.Time    `json:"date,omitempty"`
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
		date := dayOf(msg)
		if dryRun {
			ok++
			fmt.Printf("  [dry ] %s %s\n", or(date, "?"), truncate(msg.Subject, 60))
			return nil
		}
		if err := os.WriteFile(mdPath, []byte(renderMessageMD(msg, body)), 0o644); err != nil {
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
			text := ConvertAttachment(src, name, a.ContentType, ocrEnabled)
			if text == "" {
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

// dayOf is the YYYY-MM-DD date for a message, preferring the original Date.
func dayOf(msg Message) string {
	d := msg.Date
	if d.IsZero() {
		d = msg.ReceivedAt
	}
	if d.IsZero() {
		return ""
	}
	return d.UTC().Format("2006-01-02")
}

// renderMessageMD produces the message.md frontmatter + body.
func renderMessageMD(msg Message, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %q\n", msg.ID)
	fmt.Fprintf(&b, "folder: %q\n", msg.Folder)
	fmt.Fprintf(&b, "source: %q\n", msg.Source)
	fmt.Fprintf(&b, "subject: %q\n", msg.Subject)
	fmt.Fprintf(&b, "from: %q\n", msg.From)
	fmt.Fprintf(&b, "to: %q\n", msg.To)
	fmt.Fprintf(&b, "date: %q\n", dayOf(msg))
	b.WriteString("type: mail\n")
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "# %s\n\n", or(msg.Subject, "(no subject)"))
	b.WriteString(body)
	b.WriteString("\n")
	return b.String()
}

// FromEML converts raw .eml files under root to message.md (+ attachment .md).
// Unlike FromRaw (message.json), it reads the raw MIME email via enmime so the
// original Date and MIME-typed attachments are preserved.
func FromEML(root string, ocrEnabled, force, dryRun bool) (ok, skip, fail int, err error) {
	err = filepath.Walk(root, func(p string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		if !strings.EqualFold(filepath.Ext(p), ".eml") {
			return nil
		}
		msgDir := filepath.Dir(p)
		id := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		// Layout <root>/<folder>/<id>/<id>.eml: the folder is the grandparent
		// of the .eml (mbox2eml + sync --raw both nest one dir per message).
		// Flat layout (<root>/*.eml) keeps the immediate dir as before.
		folder := filepath.Base(msgDir)
		if msgDir != root {
			folder = filepath.Base(filepath.Dir(msgDir))
		}
		mdPath := filepath.Join(msgDir, "message.md")
		if !force {
			if st, err := os.Stat(mdPath); err == nil && st.Size() > 0 {
				skip++
				return nil
			}
		}
		f, err := os.Open(p)
		if err != nil {
			fail++
			fmt.Fprintf(os.Stderr, "  [fail] %s: %v\n", msgDir, err)
			return nil
		}
		env, err := enmime.ReadEnvelope(f)
		f.Close()
		if err != nil {
			fail++
			fmt.Fprintf(os.Stderr, "  [fail] %s: %v\n", msgDir, err)
			return nil
		}
		date, _ := env.Date()
		msg := Message{
			Source: "raw-email", ID: id, Folder: folder,
			Subject:  env.GetHeader("Subject"),
			From:     env.GetHeader("From"),
			To:       env.GetHeader("To"),
			CC:       env.GetHeader("Cc"),
			Date:     date,
			TextBody: env.Text,
			HTMLBody: env.HTML,
		}
		body := BodyMarkdown(msg)
		if dryRun {
			ok++
			fmt.Printf("  [dry ] %s %s\n", or(dayOf(msg), "?"), truncate(msg.Subject, 60))
			return nil
		}
		if err := os.WriteFile(mdPath, []byte(renderMessageMD(msg, body)), 0o644); err != nil {
			fail++
			return nil
		}
		attDir := filepath.Join(msgDir, "attachments")
		if err := writeEMLAttachments(attDir, env, ocrEnabled); err != nil {
			fail++
			fmt.Fprintf(os.Stderr, "  [fail] %s: %v\n", msgDir, err)
			return nil
		}
		ok++
		fmt.Printf("  [ok  ] %s %s\n", or(dayOf(msg), "?"), truncate(msg.Subject, 60))
		return nil
	})
	return ok, skip, fail, err
}

// writeEMLAttachments writes each decoded MIME part to disk and converts it to
// markdown through the type-handler registry.
func writeEMLAttachments(attDir string, env *enmime.Envelope, doOCR bool) error {
	parts := make([]*enmime.Part, 0, len(env.Attachments)+len(env.Inlines))
	parts = append(parts, env.Attachments...)
	parts = append(parts, env.Inlines...)
	for i, part := range parts {
		name := part.FileName
		if name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		name = filepath.Base(name)
		if err := os.MkdirAll(attDir, 0o755); err != nil {
			return err
		}
		rawPath := filepath.Join(attDir, name)
		if err := os.WriteFile(rawPath, part.Content, 0o644); err != nil {
			return err
		}
		text := ConvertAttachment(rawPath, name, part.ContentType, doOCR)
		if text == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(attDir, name+".md"), []byte(text), 0o644); err != nil {
			return err
		}
	}
	return nil
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
