package incubator

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strings"
)

// CanonMessageID normalizes a raw Message-ID header value into the contract
// dedup key (issue #252): first whitespace token, surrounding <> stripped,
// lowercased. Lowercase is mandated by the epic #250 contract (gator
// kind=mail), not by RFC 5322 — ids are compared canonically, not byte-wise.
func CanonMessageID(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, " \t\r\n"); i >= 0 {
		v = v[:i]
	}
	v = strings.TrimPrefix(v, "<")
	v = strings.TrimSuffix(v, ">")
	return strings.ToLower(v)
}

// MessageID extracts the canonical Message-ID of a raw .eml. has=false when
// the header is absent (caller falls back to the body hash, issue #252 п.1).
// A corrupt envelope is an error — the caller skips the file.
//
// net/mail (stdlib) reads only the header block and never decodes MIME
// charsets, so legacy bodies declaring exotic charsets (iso-8859-15 — 288
// messages of the гдеgroup corpus) cannot fail the extraction. Full MIME
// parsing is the mailconv pipeline's job (emersion/go-message); the
// incubator only needs the Message-ID header.
func MessageID(raw []byte) (id string, has bool, err error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", false, fmt.Errorf("incubator: parse eml header: %w", err)
	}
	v := msg.Header.Get("Message-Id")
	if v == "" {
		return "", false, nil
	}
	return CanonMessageID(v), true, nil
}

// Key returns the dedup key of one message: the canonical Message-ID, or a
// sha256 of the whole raw message ("body-sha256:<hex>") when the header is
// missing. The fallback prefix keeps body-hash keys from ever colliding with
// a Message-ID.
func Key(raw []byte) (key string, hasID bool, err error) {
	mid, has, err := MessageID(raw)
	if err != nil {
		return "", false, err
	}
	if has {
		return mid, true, nil
	}
	sum := sha256.Sum256(raw)
	return "body-sha256:" + hex.EncodeToString(sum[:]), false, nil
}
