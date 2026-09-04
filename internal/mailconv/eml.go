package mailconv

import (
	"io"
	"strings"
	"time"

	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

var zeroTime time.Time

// attachmentPart is one decoded MIME attachment leaf of a raw email.
type attachmentPart struct {
	FileName    string
	ContentType string
	Content     []byte
}

// parsedEML is the result of parsing one raw .eml: a Message plus any
// attachment parts that must be written to disk.
type parsedEML struct {
	msg   Message
	parts []attachmentPart
}

// parseEML decodes a raw MIME email into a Message + attachments using
// emersion/go-message. entity.Walk() recurses through nested multipart
// (mixed/alternative) so text/html bodies and every attachment leaf are found
// regardless of nesting depth. Transfer-encoding (base64/quoted-printable) and
// charset (via the blank-imported go-message/charset → x/text) are decoded by
// Entity.Body.
func parseEML(r io.Reader) (parsedEML, error) {
	root, err := message.Read(r)
	if err != nil {
		return parsedEML{}, err
	}
	mailReader := mail.NewReader(root)

	var res parsedEML
	date, err := mailReader.Header.Date()
	if err != nil {
		date = zeroTime
	}
	res.msg = Message{
		Source:  "raw-email",
		Subject: headerText(mailReader.Header, "Subject"),
		From:    headerText(mailReader.Header, "From"),
		To:      headerText(mailReader.Header, "To"),
		CC:      headerText(mailReader.Header, "Cc"),
		Date:    date,
	}

	err = root.Walk(func(_ []int, ent *message.Entity, _ error) error {
		mt, params, _ := ent.Header.ContentType()
		if strings.HasPrefix(mt, "multipart/") {
			return nil // container; Walk recurses into its parts
		}
		body, rerr := io.ReadAll(ent.Body)
		if rerr != nil {
			return nil
		}
		name := partName(params, ent)
		if isAttachment(ent, name) {
			res.parts = append(res.parts, attachmentPart{
				FileName:    name,
				ContentType: mt,
				Content:     body,
			})
			return nil
		}
		switch {
		case strings.HasPrefix(mt, "text/plain"):
			if res.msg.TextBody != "" {
				res.msg.TextBody += "\n"
			}
			res.msg.TextBody += strings.TrimRight(string(body), "\r\n")
			return nil
		case strings.HasPrefix(mt, "text/html"):
			res.msg.HTMLBody += string(body)
			return nil
		}
		return nil
	})
	if err != nil {
		return parsedEML{}, err
	}
	if len(res.parts) > 0 {
		res.msg.HasAttachments = true
		res.msg.Attachments = make([]Attachment, 0, len(res.parts))
		for _, p := range res.parts {
			res.msg.Attachments = append(res.msg.Attachments, Attachment{
				FileName:    p.FileName,
				StoredName:  p.FileName,
				Size:        int64(len(p.Content)),
				ContentType: p.ContentType,
			})
		}
	}
	return res, nil
}

// headerText decodes an RFC2047 + charset header field to a plain string.
func headerText(h mail.Header, key string) string {
	s, err := h.Text(key)
	if err != nil {
		return strings.TrimSpace(h.Get(key))
	}
	return s
}

// partName returns the filename for an attachment leaf, preferring the
// Content-Disposition filename, then the Content-Type name param.
func partName(params map[string]string, ent *message.Entity) string {
	if name, ok := params["name"]; ok && name != "" {
		return name
	}
	if disp, dparams, err := ent.Header.ContentDisposition(); err == nil && disp != "" {
		if name, ok := dparams["filename"]; ok && name != "" {
			return name
		}
	}
	return ""
}

// isAttachment reports whether a leaf is an attachment (vs a body part): it has
// a filename, or is declared as an attachment/inline disposition. A body part
// has neither and is treated as message content.
func isAttachment(ent *message.Entity, name string) bool {
	if name != "" {
		return true
	}
	if disp, _, err := ent.Header.ContentDisposition(); err == nil {
		switch strings.ToLower(disp) {
		case "attachment", "inline":
			return true
		}
	}
	return false
}
