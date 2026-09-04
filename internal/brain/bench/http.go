package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/eSlider/2dph/pkg/utils"
)

// HTTPConfig configures the MCP HTTP searcher (the live brain serve process).
type HTTPConfig struct {
	// BaseURL is the brain HTTP base, e.g. "http://127.0.0.1:8630".
	BaseURL string
	// Timeout per search call; zero uses 120s (baseline is ~32s/query).
	Timeout time.Duration
}

// HTTPSearcher calls the brain MCP JSON-RPC "search" tool over HTTP
// (POST {base}/mcp). Read-only: the server side holds the DB, so the bench
// never touches the single-writer file directly (issue #202 constraint).
type HTTPSearcher struct {
	hc  *http.Client
	cfg HTTPConfig
}

// rpcRequest is the JSON-RPC envelope; request and response are strict structs
// (no map[string]any at the boundary).
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

type rpcResponse struct {
	Result *struct {
		Content []struct {
			Text    string `json:"text"`
			IsError bool   `json:"isError,omitempty"`
		} `json:"content"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// NewHTTPSearcher returns a nil-safe client for a brain MCP endpoint.
func NewHTTPSearcher(cfg HTTPConfig) *HTTPSearcher {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "http://127.0.0.1:8630"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &HTTPSearcher{
		hc:  &http.Client{Timeout: timeout},
		cfg: HTTPConfig{BaseURL: base, Timeout: timeout},
	}
}

// Search runs the MCP search tool with noweb=true (offline harness).
func (s *HTTPSearcher) Search(ctx context.Context, query string, limit int) ([]Hit, error) {
	req := rpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call"}
	req.Params.Name = "search"
	req.Params.Arguments = map[string]any{"q": query, "n": limit, "noweb": true}
	var resp rpcResponse
	if err := utils.DoJSON(ctx, s.hc, http.MethodPost,
		s.cfg.BaseURL+"/mcp", req, &resp); err != nil {
		return nil, fmt.Errorf("mcp search %q: %w", query, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("mcp search %q: rpc error: %s", query, resp.Error.Message)
	}
	if resp.Result == nil {
		return nil, fmt.Errorf("mcp search %q: empty result", query)
	}
	for _, c := range resp.Result.Content {
		if c.IsError {
			return nil, fmt.Errorf("mcp search %q: %s", query, trimErr(c.Text))
		}
	}
	if len(resp.Result.Content) == 0 {
		return nil, fmt.Errorf("mcp search %q: no content", query)
	}
	var out searchOut
	if err := json.Unmarshal([]byte(resp.Result.Content[0].Text), &out); err != nil {
		return nil, fmt.Errorf("mcp search %q: parse hits: %w", query, err)
	}
	hits := make([]Hit, 0, len(out.Results))
	for _, r := range out.Results {
		hits = append(hits, Hit{ID: r.ID, Text: r.Text, Root: r.Root})
	}
	return hits, nil
}

func (s *HTTPSearcher) Close() error { return nil }

func (s *HTTPSearcher) Name() string { return s.cfg.BaseURL }
