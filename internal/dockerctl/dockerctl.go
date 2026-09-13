// Package dockerctl is a minimal Docker Engine API client over the local unix
// socket, enough for the index-sync cycle to quiesce and restart the compose
// brain service: find one container by compose labels, stop it, start it,
// inspect its running/health state. Stdlib only — the full docker SDK is a
// heavy dependency for four endpoints (go-research: reuse first, but YAGNI).
package dockerctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// DefaultSocket is the Docker daemon socket path mounted into the loop service.
const DefaultSocket = "/var/run/docker.sock"

// Client talks to the Docker Engine API.
type Client struct {
	base *url.URL
	hc   *http.Client
}

// New returns a client bound to a unix socket (DefaultSocket when empty).
func New(socket string) *Client {
	if socket == "" {
		socket = DefaultSocket
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &Client{
		base: &url.URL{Scheme: "http", Host: "docker"},
		hc:   &http.Client{Transport: tr, Timeout: 60 * time.Second},
	}
}

// NewHTTP returns a client against a concrete base URL with a caller HTTP
// client. Used by tests against an httptest server (real HTTP).
func NewHTTP(base string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("dockerctl: base url: %w", err)
	}
	return &Client{base: u, hc: hc}, nil
}

// Ping reports whether the daemon answers /_ping.
func (c *Client) Ping(ctx context.Context) error {
	_, status, err := c.do(ctx, http.MethodGet, "/_ping", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("dockerctl: ping status %d", status)
	}
	return nil
}

// FindOne returns the id of the single container matching every label
// (key=value). Zero or several matches is an error: the loop must target one
// deterministic container.
func (c *Client) FindOne(ctx context.Context, labels map[string]string) (string, error) {
	filters := map[string][]string{}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		filters["label"] = append(filters["label"], k+"="+labels[k])
	}
	raw, err := json.Marshal(filters)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("all", "1")
	q.Set("filters", string(raw))
	body, status, err := c.do(ctx, http.MethodGet, "/containers/json?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("dockerctl: list containers status %d: %s", status, strings.TrimSpace(string(body)))
	}
	var list []struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return "", fmt.Errorf("dockerctl: decode containers: %w", err)
	}
	if len(list) != 1 {
		return "", fmt.Errorf("dockerctl: %d containers match %v, want exactly 1", len(list), labels)
	}
	return list[0].ID, nil
}

// Stop stops a container, waiting up to timeoutSec for it to exit.
func (c *Client) Stop(ctx context.Context, id string, timeoutSec int) error {
	if timeoutSec < 0 {
		timeoutSec = 0
	}
	q := url.Values{}
	q.Set("t", fmt.Sprint(timeoutSec))
	body, status, err := c.do(ctx, http.MethodPost, "/containers/"+id+"/stop?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	// 304 = already stopped.
	if status != http.StatusNoContent && status != http.StatusNotModified {
		return fmt.Errorf("dockerctl: stop %s status %d: %s", id, status, strings.TrimSpace(string(body)))
	}
	return nil
}

// Start starts a container. 304 (already running) is success.
func (c *Client) Start(ctx context.Context, id string) error {
	body, status, err := c.do(ctx, http.MethodPost, "/containers/"+id+"/start", nil)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent && status != http.StatusNotModified {
		return fmt.Errorf("dockerctl: start %s status %d: %s", id, status, strings.TrimSpace(string(body)))
	}
	return nil
}

// Inspect reports whether the container is running and its health status
// (empty when the container declares no healthcheck).
func (c *Client) Inspect(ctx context.Context, id string) (running bool, health string, err error) {
	body, status, err := c.do(ctx, http.MethodGet, "/containers/"+id+"/json", nil)
	if err != nil {
		return false, "", err
	}
	if status != http.StatusOK {
		return false, "", fmt.Errorf("dockerctl: inspect %s status %d: %s", id, status, strings.TrimSpace(string(body)))
	}
	var info struct {
		State struct {
			Running bool `json:"Running"`
			Health  *struct {
				Status string `json:"Status"`
			} `json:"Health"`
		} `json:"State"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return false, "", fmt.Errorf("dockerctl: decode inspect: %w", err)
	}
	if info.State.Health != nil {
		health = info.State.Health.Status
	}
	return info.State.Running, health, nil
}

// WaitHealthy polls Inspect until the container is running (and healthy when
// it declares a healthcheck) or the deadline passes.
func (c *Client) WaitHealthy(ctx context.Context, id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		running, health, err := c.Inspect(ctx, id)
		if err == nil && running && (health == "" || health == "healthy") {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("dockerctl: container %s not healthy within %s (running=%v health=%q err=%v)", id, timeout, running, health, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) ([]byte, int, error) {
	raw := strings.TrimRight(c.base.String(), "/") + path
	req, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		return nil, 0, fmt.Errorf("dockerctl: request: %w", err)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("dockerctl: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("dockerctl: read body: %w", err)
	}
	return b, resp.StatusCode, nil
}
