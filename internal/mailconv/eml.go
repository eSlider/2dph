package mailconv

import (
	"errors"
	"io"
)

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

// parseEML decodes a raw MIME email into a Message + attachments.
func parseEML(r io.Reader) (parsedEML, error) {
	return parsedEML{}, errors.New("parseEML: not implemented")
}
