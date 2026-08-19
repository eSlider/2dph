// Package sync downloads OnlyOffice and Gmail messages to var/mail/ as raw
// JSON + attachment files, then hands off to bin/mail/import --from-raw for
// markdown conversion.
//
// On-disk schema (per message):
//
//	var/mail/<folder>/<id>/message.json   # Message (this package)
//	var/mail/<folder>/<id>/attachments/    # raw attachment bytes (storedName)
//
// The Message JSON is the contract shared with the Go converter. Fields
// deliberately mirror what bin/mail/import already reads from the OnlyOffice
// API, so conversion is source-agnostic.
package sync

import (
	"time"
)

// Attachment describes one attachment of a Message. FileID/FileName/StoredName
// mirror OnlyOffice; Gmail fills them from its own ids. StoredName is always
// unique (hash/attachment id) so raw files never collide.
type Attachment struct {
	FileID      string `json:"fileId,omitempty"`
	FileName    string `json:"fileName"`
	StoredName  string `json:"storedName"`
	Size        int64  `json:"size,omitempty"`
	ContentType string `json:"contentType,omitempty"`
}

// Message is the normalized record written to var/mail/<folder>/<id>/message.json.
type Message struct {
	Source         string       `json:"source"` // "onlyoffice" | "gmail"
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
	MimeMessageID  string       `json:"mimeMessageId,omitempty"`
}
