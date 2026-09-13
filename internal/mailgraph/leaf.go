package mailgraph

import (
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/contract"
)

// RowToLeaf maps a gator kind=mail row to a searchable contract.Leaf (ADR-0013
// §2, issue #297). Tombstone rows are excluded upstream by ReadSQL; empty
// message_id or content-less text are skipped.
func RowToLeaf(r Row) (contract.Leaf, bool) {
	if r.MessageID == "" {
		return contract.Leaf{}, false
	}
	text := leafText(r.Subject, r.Body)
	if contract.NormalizeText(text) == "" {
		return contract.Leaf{}, false
	}
	observed := ""
	if !r.Date.IsZero() {
		observed = r.Date.UTC().Format(time.RFC3339)
	}
	return contract.Leaf{
		Source:     "mail",
		ExternalID: r.MessageID,
		Kind:       "mail",
		Text:       text,
		Root:       "info",
		Confidence: "confirmed",
		How:        "gator/mail",
		Loc:        r.gatorRef(),
		ObservedAt: observed,
	}, true
}

func leafText(subject, body string) string {
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)
	switch {
	case subject == "":
		return body
	case body == "":
		return subject
	default:
		return subject + "\n\n" + body
	}
}
