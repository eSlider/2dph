package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, OpenAPI())
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST JSON-RPC"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "read body"})
		return
	}
	var req rpcReq
	if err := json.Unmarshal(raw, &req); err != nil {
		writeJSON(w, http.StatusOK, rpcResult(nil, nil, &rpcErr{-32700, "parse error"}))
		return
	}
	result, rpcErrv, callErr := s.mcpDispatch(r, req)
	if callErr != nil {
		writeJSON(w, http.StatusOK, rpcResult(req.ID, nil, &rpcErr{-32603, callErr.Error()}))
		return
	}
	writeJSON(w, http.StatusOK, rpcResult(req.ID, result, rpcErrv))
}

func (s *Server) mcpDispatch(r *http.Request, req rpcReq) (any, *rpcErr, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "2dph", "version": "1"},
		}, nil, nil
	case "notifications/initialized", "notifications/cancelled":
		return map[string]any{}, nil, nil
	case "tools/list":
		return map[string]any{"tools": MCPTools()}, nil, nil
	case "tools/call":
		out, err := s.mcpCall(r, req.Params)
		return out, nil, err
	case "ping":
		return map[string]any{}, nil, nil
	default:
		return nil, &rpcErr{-32601, "method not found"}, nil
	}
}

func (s *Server) mcpCall(r *http.Request, params json.RawMessage) (any, error) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("params")
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}
	var (
		body []byte
		err  error
	)
	switch p.Name {
	case "search":
		q := strings.TrimSpace(fmt.Sprint(p.Arguments["q"]))
		if q == "" || q == "<nil>" {
			return mcpText(`{"error":"q required"}`, true), nil
		}
		limit := 10
		if raw, ok := p.Arguments["n"]; ok {
			switch n := raw.(type) {
			case float64:
				limit = int(n)
			case string:
				if v, e := strconv.Atoi(n); e == nil {
					limit = v
				}
			}
		}
		if limit < 1 || limit > 100 {
			return mcpText(`{"error":"n must be int 1..100"}`, true), nil
		}
		asOf := ""
		if raw, ok := p.Arguments["as_of"]; ok {
			asOf = strings.TrimSpace(fmt.Sprint(raw))
			if asOf == "<nil>" {
				asOf = ""
			}
		}
		if !s.tryAcquire(r) {
			return nil, fmt.Errorf("cancelled")
		}
		defer s.release()
		body, err = s.api.Search(r.Context(), q, limit, asOf)
	case "get":
		id := strings.TrimSpace(fmt.Sprint(p.Arguments["id"]))
		if id == "" || id == "<nil>" {
			return mcpText(`{"error":"id required"}`, true), nil
		}
		full := false
		switch v := p.Arguments["body"].(type) {
		case bool:
			full = v
		case string:
			full = v == "1" || v == "true"
		}
		if !s.tryAcquire(r) {
			return nil, fmt.Errorf("cancelled")
		}
		defer s.release()
		body, err = s.api.Get(r.Context(), id, full)
	case "stats":
		if !s.tryAcquire(r) {
			return nil, fmt.Errorf("cancelled")
		}
		defer s.release()
		body, err = s.api.Stats(r.Context())
	case "audit":
		if !s.tryAcquire(r) {
			return nil, fmt.Errorf("cancelled")
		}
		defer s.release()
		body, err = s.api.Audit(r.Context())
	case "ingest":
		if !s.tryAcquire(r) {
			return nil, fmt.Errorf("cancelled")
		}
		defer s.release()
		var payload []byte
		text := strings.TrimSpace(fmt.Sprint(p.Arguments["text"]))
		if text != "" && text != "<nil>" {
			payload, err = json.Marshal(p.Arguments)
			if err != nil {
				return nil, err
			}
		}
		body, err = s.api.Ingest(r.Context(), payload)
	default:
		return nil, fmt.Errorf("unknown tool %s", p.Name)
	}
	if err != nil {
		return mcpText(err.Error(), true), nil
	}
	return mcpText(string(body), false), nil
}

func mcpText(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
		"isError": isError,
	}
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

func rpcResult(id json.RawMessage, result any, err *rpcErr) rpcResp {
	out := rpcResp{JSONRPC: "2.0", ID: id}
	if len(id) == 0 {
		out.ID = []byte("null")
	}
	if err != nil {
		out.Error = err
		return out
	}
	if result == nil {
		result = map[string]any{}
	}
	out.Result = result
	return out
}
