package cron

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ingestPayload struct {
	Leafs []Leaf `json:"leafs"`
}

// Push POSTs leafs to <brain>/ingest as a single {leafs:[...]} payload and
// returns how many were accepted. A non-2xx response or transport error
// surfaces as an error; no leafs are considered ingested on failure.
// timeout bounds the request; the brain embeds every leaf server-side, so a
// full corpus needs minutes.
func Push(ctx context.Context, brain string, timeout time.Duration, leafs []Leaf) (int, error) {
	if len(leafs) == 0 {
		return 0, nil
	}
	payload, err := json.Marshal(ingestPayload{Leafs: leafs})
	if err != nil {
		return 0, fmt.Errorf("encode leafs: %w", err)
	}
	url := strings.TrimSuffix(brain, "/") + "/ingest"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("brain %s: %w", url, err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("brain %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("brain ingest: HTTP %d", resp.StatusCode)
	}
	return len(leafs), nil
}
