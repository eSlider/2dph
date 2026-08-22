package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DoJSON performs an HTTP request with an optional JSON body, checks for a 2xx
// status, and decodes the JSON response into dest (a pointer). A nil body sends
// no payload; a nil dest skips decoding. opts let callers set headers (e.g.
// Basic auth). The error carries the HTTP status and a short body snippet so
// callers can surface remote errors verbatim.
func DoJSON(ctx context.Context, client *http.Client, method, url string, body, dest any, opts ...func(*http.Request)) error {
	if client == nil {
		client = http.DefaultClient
	}
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, opt := range opts {
		opt(req)
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", res.StatusCode, Snippet(string(raw), 300))
	}
	if dest != nil {
		if err := json.Unmarshal(raw, dest); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// GetJSON is DoJSON with the GET method and no request body.
func GetJSON(ctx context.Context, client *http.Client, url string, dest any, opts ...func(*http.Request)) error {
	return DoJSON(ctx, client, http.MethodGet, url, nil, dest, opts...)
}
