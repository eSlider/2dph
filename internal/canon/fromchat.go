package canon

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// FromChat builds the canonical Message from a chat record. platform is the
// chat platform ("telegram","whatsapp","linkedin"), threadID the conversation
// id, text the message body, from the sender handle, timestamp an RFC3339 (or
// YYYY-MM-DDTHH:MM:SS) value, and replyTo the id this message replies to (nil
// for a top-level message).
func FromChat(platform, threadID, text, from, timestamp string, replyTo *string) (*Message, error) {
	m := &Message{
		ID:       chatID(platform, threadID, text, from),
		ThreadID: threadID,
		Platform: platform,
		From:     Person{ID: platform + ":" + from, Name: from, Handle: from},
		Body:     text,
		ReplyTo:  replyTo,
	}
	if timestamp != "" {
		t, err := parseChatTime(timestamp)
		if err != nil {
			return nil, err
		}
		m.SentAt = t
	}
	return m, nil
}

func parseChatTime(s string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, &time.ParseError{Layout: time.RFC3339, Value: s}
}

func chatID(platform, threadID, text, from string) string {
	sum := sha256.Sum256([]byte(platform + "|" + threadID + "|" + from + "|" + text))
	return hex.EncodeToString(sum[:8])
}
