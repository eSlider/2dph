package chat

import (
	"context"
	"fmt"
	"os"
	"time"
)

type Source interface {
	Name() string
	Sync(ctx context.Context, outDir string, limit int) error
}

type Message struct {
	ID        string  `json:"id"`
	Timestamp string  `json:"ts"`
	From      string  `json:"from"`
	Text      string  `json:"text"`
	Media     *string `json:"media,omitempty"`
	Platform  string  `json:"platform"`
}

type ChatInfo struct {
	ID           string   `json:"id"`
	Platform     string   `json:"platform"`
	Name         string   `json:"name"`
	Participants []string `json:"participants"`
	Type         string   `json:"type"`
	MessageCount int      `json:"messageCount"`
	LastTS       string   `json:"lastTs,omitempty"`
}

func envVar(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseSince(s string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse --since %q; use YYYY-MM-DD or RFC3339", s)
}
