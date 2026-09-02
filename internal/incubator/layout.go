package incubator

import (
	"bytes"
	"fmt"
	"net/mail"
	"net/textproto"
	"strings"
)

// Target mailboxes of the owner-address routing model (decision 2026-09-02,
// issue #252): the incubator mailbox IS the owner's historical address, so
// every message is filed like in a real account:
//   - LayoutSent — the owner is in From (outgoing mail);
//   - LayoutInbox — the owner is in To/CC/Delivered-To (incoming mail);
//   - LayoutUnmatched — the owner is in none of them (foreign/ticket
//     mailings); quarantined for triage, never silently dropped.
const (
	LayoutSent      = "Sent"
	LayoutInbox     = "INBOX"
	LayoutUnmatched = "INBOX/Unmatched"
)

// LayoutOf routes one raw .eml by the owner's historical address. Only the
// header block is read (net/mail never decodes MIME charsets — legacy bodies
// cannot fail the routing, same rationale as MessageID in messageid.go).
func LayoutOf(raw []byte, owner string) (string, error) {
	if owner == "" {
		return "", fmt.Errorf("incubator: layout requires an owner address")
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("incubator: parse eml header: %w", err)
	}
	if headerHasOwner(msg.Header, "From", owner) {
		return LayoutSent, nil
	}
	for _, f := range []string{"To", "Cc", "Delivered-To"} {
		if headerHasOwner(msg.Header, f, owner) {
			return LayoutInbox, nil
		}
	}
	return LayoutUnmatched, nil
}

// headerHasOwner reports whether any value of the header field contains the
// owner address. Parsed address lists are authoritative: the addr-spec is
// compared case-insensitively, display names never match. When a value does
// not parse as an address list (legacy route/comment syntax), a raw substring
// match on the value is the fallback — so legacy mail is attributed to the
// owner instead of being misrouted to Unmatched.
func headerHasOwner(h mail.Header, field, owner string) bool {
	owner = strings.ToLower(owner)
	// mail.Header is a textproto.MIMEHeader; Values() unfolds and returns
	// every occurrence of the field (Delivered-To legitimately repeats).
	for _, v := range textproto.MIMEHeader(h).Values(field) {
		if al, err := mail.ParseAddressList(v); err == nil {
			for _, a := range al {
				if strings.EqualFold(a.Address, owner) {
					return true
				}
			}
			continue
		}
		if strings.Contains(strings.ToLower(v), owner) {
			return true
		}
	}
	return false
}
