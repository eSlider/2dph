package oohtml

import (
	"fmt"
	"io"
	"time"

	"github.com/emersion/go-message"
)

// BuildMessage writes a full RFC 5322 MIME message with the branded HTML body
// and the logo embedded as an inline part referenced by Content-ID (cid). The
// multipart/related container is the email-safe way to attach inline images:
// clients resolve <img src="cid:produktor-logo.gif"> locally instead of loading
// a remote URL (which mail clients block).
//
// logo holds the committed GIF bytes (see LogoPath).
func BuildMessage(w io.Writer, p SendParams, html string, logo []byte) error {
	header := message.Header{}
	header.SetText("From", p.From)
	header.SetText("To", p.To)
	if p.Cc != "" {
		header.SetText("Cc", p.Cc)
	}
	if p.Bcc != "" {
		header.SetText("Bcc", p.Bcc)
	}
	header.SetText("Subject", p.Subject)
	header.Set("Date", time.Now().UTC().Format(time.RFC1123Z))
	header.SetContentType("multipart/related", map[string]string{"type": "text/html"})

	mw, err := message.CreateWriter(w, header)
	if err != nil {
		return fmt.Errorf("oohtml: create mime writer: %w", err)
	}
	defer mw.Close()

	// HTML body part.
	htmlHeader := message.Header{}
	htmlHeader.SetContentType("text/html", map[string]string{"charset": "utf-8"})
	htmlHeader.Set("Content-Transfer-Encoding", "quoted-printable")
	hw, err := mw.CreatePart(htmlHeader)
	if err != nil {
		return fmt.Errorf("oohtml: create html part: %w", err)
	}
	if _, err := hw.Write([]byte(html)); err != nil {
		return fmt.Errorf("oohtml: write html part: %w", err)
	}
	if err := hw.Close(); err != nil {
		return fmt.Errorf("oohtml: close html part: %w", err)
	}

	// Logo inline part; Content-ID matches the <img src="cid:..."> reference.
	logoHeader := message.Header{}
	logoHeader.SetContentType(LogoContentType, map[string]string{"name": LogoFilename})
	logoHeader.SetContentDisposition("inline", map[string]string{"filename": LogoFilename})
	logoHeader.Set("Content-Transfer-Encoding", "base64")
	logoHeader.Set("Content-ID", "<"+LogoCID+">")
	lw, err := mw.CreatePart(logoHeader)
	if err != nil {
		return fmt.Errorf("oohtml: create logo part: %w", err)
	}
	if _, err := lw.Write(logo); err != nil {
		return fmt.Errorf("oohtml: write logo part: %w", err)
	}
	return lw.Close()
}
