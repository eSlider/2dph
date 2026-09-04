package canon

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

// FromMailFile converts a raw .eml file into the canonical Message.
func FromMailFile(path string) (*Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return FromMail(f)
}

// FromMail parses a raw MIME email into the canonical Message using
// emersion/go-message: envelope (From/To/Cc/Bcc) via the mail.Header, reply
// link + thread from References/In-Reply-To/Message-Id, and the body from
// text/plain (charset-decoded) with a text/html fallback. Attachments are
// recorded lazily (metadata only, never buffered).
func FromMail(r io.Reader) (*Message, error) {
	root, err := message.Read(r)
	if err != nil {
		return nil, err
	}
	mr := mail.NewReader(root)
	h := mr.Header

	m := &Message{Platform: "mail"}
	if t, err := h.Date(); err == nil {
		m.SentAt = t
	}
	if l, err := h.AddressList("From"); err == nil && len(l) > 0 {
		m.From = personFromAddress(l[0])
	}
	m.To = personsFromAddresses(h, "To")
	m.CC = personsFromAddresses(h, "Cc")
	m.BCC = personsFromAddresses(h, "Bcc")

	msgID := strings.TrimSpace(h.Get("Message-Id"))
	if msgID == "" {
		msgID = strings.TrimSpace(h.Get("Message-ID"))
	}
	if refs := firstRef(h.Get("References")); refs != "" {
		m.ThreadID = refs
	} else {
		m.ThreadID = msgID
	}
	if irt := firstRef(h.Get("In-Reply-To")); irt != "" {
		id := irt
		m.ReplyTo = &id
	}

	var text, html strings.Builder
	err = root.Walk(func(_ []int, ent *message.Entity, _ error) error {
		mt, params, _ := ent.Header.ContentType()
		if strings.HasPrefix(mt, "multipart/") {
			return nil
		}
		name := partName(params, ent)
		body, rerr := readEntity(ent)
		if rerr != nil {
			return nil
		}
		if name != "" || isDisp(ent) {
			m.Attachments = append(m.Attachments, Attachment{
				Name:        name,
				ContentType: mt,
				Size:        int64(len(body)),
			})
			return nil
		}
		switch {
		case strings.HasPrefix(mt, "text/plain"):
			text.WriteString(string(body))
			text.WriteString("\n")
		case strings.HasPrefix(mt, "text/html"):
			html.WriteString(string(body))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if tb := strings.TrimSpace(text.String()); tb != "" {
		m.Body = tb
	} else if hb := strings.TrimSpace(html.String()); hb != "" {
		m.Body = strings.TrimSpace(mailconv.StripHTML(hb))
	}

	if msgID != "" {
		m.ID = msgID
	} else {
		m.ID = derivedID(m)
	}
	return m, nil
}

// readEntity reads an entity body. Transfer-encoding (base64/quoted-printable)
// is already decoded by ent.Body; charset decoding for the text/plain body is
// applied via the blank-imported go-message/charset default reader.
func readEntity(ent *message.Entity) ([]byte, error) {
	return io.ReadAll(ent.Body)
}

func personFromAddress(a *mail.Address) Person {
	email := strings.ToLower(strings.TrimSpace(a.Address))
	return Person{ID: email, Name: strings.TrimSpace(a.Name), Email: email}
}

func personsFromAddresses(h mail.Header, key string) []Person {
	list, err := h.AddressList(key)
	if err != nil || len(list) == 0 {
		return nil
	}
	out := make([]Person, 0, len(list))
	for _, a := range list {
		if p := personFromAddress(a); p.ID != "" {
			out = append(out, p)
		}
	}
	return out
}

// partName returns the attachment filename (Content-Type name param, then
// Content-Disposition filename).
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

func isDisp(ent *message.Entity) bool {
	disp, _, err := ent.Header.ContentDisposition()
	return err == nil && disp != ""
}

// firstRef returns the first whitespace-delimited message-id of a References /
// In-Reply-To value.
func firstRef(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func derivedID(m *Message) string {
	sum := sha256.Sum256([]byte(m.From.ID + "|" + m.SentAt.String() + "|" + m.Body))
	return hex.EncodeToString(sum[:8])
}
