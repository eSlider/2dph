package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type LinkedInMCPSource struct {
	userDataDir string
	limit       int
}

type lnInboxItem struct {
	ThreadID         string `json:"thread_id"`
	Participants     string `json:"participants"`
	LastMessage      string `json:"last_message"`
	LastMessageDate  string `json:"last_message_date"`
	Unread           bool   `json:"unread"`
}

type lnInboxEnvelope struct {
	Results    []lnInboxItem `json:"results"`
	HasMore    bool          `json:"hasMore"`
}

type lnMessage struct {
	From       string `json:"from"`
	Date       string `json:"date"`
	Text       string `json:"text"`
}

type lnConvEnvelope struct {
	Results    []lnMessage `json:"results"`
	HasMore    bool        `json:"hasMore"`
	TotalCount int         `json:"total_count"`
}

func NewLinkedInMCPSource(userDataDir string) *LinkedInMCPSource {
	return &LinkedInMCPSource{userDataDir: userDataDir}
}

func (s *LinkedInMCPSource) Name() string { return "linkedin" }

func (s *LinkedInMCPSource) Sync(ctx context.Context, outDir string, limit int) error {
	if limit > 0 {
		s.limit = limit
	}

	client, err := newLinkedInMCP(ctx, s.userDataDir)
	if err != nil {
		return fmt.Errorf("linkedin mcp: %w", err)
	}
	defer client.Close()

	inbox, err := client.GetInbox(ctx, 50)
	if err != nil {
		return fmt.Errorf("get_inbox: %w", err)
	}
	if len(inbox) == 0 {
		fmt.Println("chats: no LinkedIn conversations found")
		return nil
	}

	fmt.Printf("chats: found %d LinkedIn conversations\n", len(inbox))

	for _, conv := range inbox {
		convID := sanitizeDir(conv.ThreadID)
		if convID == "" {
			convID = fmt.Sprintf("conv_%d", time.Now().UnixNano())
		}

		parts := strings.SplitN(conv.Participants, ",", 2)
		chatName := strings.TrimSpace(parts[0])
		if chatName == "" {
			chatName = convID
		}

		chatDir := filepath.Join(outDir, "linkedin", convID)
		if err := os.MkdirAll(chatDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "chats: mkdir %s: %v\n", chatDir, err)
			continue
		}

		msgLimit := 100
		if s.limit > 0 {
			msgLimit = s.limit
		}

		msgs, err := client.GetConversation(ctx, "", conv.ThreadID, msgLimit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chats: get_conversation %s: %v\n", convID, err)
			continue
		}

		jsonlPath := filepath.Join(chatDir, "messages.jsonl")
		f, err := os.Create(jsonlPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chats: create %s: %v\n", jsonlPath, err)
			continue
		}

		enc := json.NewEncoder(f)
		written := 0
		for i, m := range msgs {
			text := m.Text
			if text == "" {
				continue
			}

			ts := m.Date
			if t, err := time.Parse("2006-01-02T15:04:05Z07:00", m.Date); err == nil {
				ts = t.UTC().Format(time.RFC3339)
			} else if t, err := time.Parse(time.RFC3339, m.Date); err == nil {
				ts = t.UTC().Format(time.RFC3339)
			}

			chatMsg := Message{
				ID:        fmt.Sprintf("li_%s_%d", convID, i),
				Timestamp: ts,
				From:      m.From,
				Text:      text,
				Platform:  "linkedin",
			}
			if err := enc.Encode(chatMsg); err != nil {
				fmt.Fprintf(os.Stderr, "chats: encode: %v\n", err)
				continue
			}
			written++
		}
		f.Close()

		fmt.Printf("chats: synced %s (%s) — %d messages\n", chatName, convID, written)
	}

	return nil
}

type linkedInMCPClient struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Scanner
	msgID  int
}

func newLinkedInMCP(ctx context.Context, userDataDir string) (*linkedInMCPClient, error) {
	args := []string{
		"mcp-server-linkedin@latest",
		"--user-data-dir", userDataDir,
		"--no-auto-import",
		"--transport", "stdio",
		"--login-timeout", "10",
		"--browser-wait", "1",
		"--browser-idle-timeout", "10",
		"--log-level", "ERROR",
	}

	cmd := exec.CommandContext(ctx, "uvx", args...)
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	c := &linkedInMCPClient{
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdin),
		stdout: bufio.NewScanner(stdout),
		msgID:  0,
	}
	c.stdout.Buffer(make([]byte, 1<<20), 1<<20)

	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("init: %w", err)
	}
	return c, nil
}

func (c *linkedInMCPClient) nextID() int {
	c.msgID++
	return c.msgID
}

func (c *linkedInMCPClient) initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "chats-sync",
			"version": "0.1.0",
		},
	}
	_, err := c.send(ctx, "initialize", params)
	return err
}

func (c *linkedInMCPClient) send(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID()
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if _, err := c.stdin.Write(body); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	if err := c.stdin.WriteByte('\n'); err != nil {
		return nil, fmt.Errorf("newline: %w", err)
	}
	if err := c.stdin.Flush(); err != nil {
		return nil, fmt.Errorf("flush: %w", err)
	}

	for c.stdout.Scan() {
		line := c.stdout.Text()
		if line == "" {
			continue
		}
		var resp struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Result  json.RawMessage `json:"result,omitempty"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error,omitempty"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return nil, fmt.Errorf("unmarshal: %w\nline: %s", err, line[:min(len(line), 500)])
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
	return nil, fmt.Errorf("no response: %w", c.stdout.Err())
}

func (c *linkedInMCPClient) GetInbox(ctx context.Context, limit int) ([]lnInboxItem, error) {
	params := map[string]interface{}{
		"limit": limit,
	}
	result, err := c.send(ctx, "tools/call", map[string]interface{}{
		"name":      "get_inbox",
		"arguments": params,
	})
	if err != nil {
		return nil, err
	}

	var toolRes struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &toolRes); err != nil {
		return nil, fmt.Errorf("unmarshal tool: %w", err)
	}
	if toolRes.IsError {
		return nil, fmt.Errorf("get_inbox error")
	}
	if len(toolRes.Content) == 0 {
		return nil, nil
	}

	text := toolRes.Content[0].Text
	var env lnInboxEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		var arr []lnInboxItem
		if err2 := json.Unmarshal([]byte(text), &arr); err2 == nil {
			return arr, nil
		}
		return nil, fmt.Errorf("parse inbox: %w", err)
	}
	return env.Results, nil
}

func (c *linkedInMCPClient) GetConversation(ctx context.Context, username, threadID string, limit int) ([]lnMessage, error) {
	params := map[string]interface{}{
		"linkedin_username": username,
		"thread_id":         threadID,
		"index":             limit,
	}
	result, err := c.send(ctx, "tools/call", map[string]interface{}{
		"name":      "get_conversation",
		"arguments": params,
	})
	if err != nil {
		return nil, err
	}

	var toolRes struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &toolRes); err != nil {
		return nil, fmt.Errorf("unmarshal tool: %w", err)
	}
	if toolRes.IsError {
		return nil, nil
	}
	if len(toolRes.Content) == 0 {
		return nil, nil
	}

	text := toolRes.Content[0].Text
	var env lnConvEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		var arr []lnMessage
		if err2 := json.Unmarshal([]byte(text), &arr); err2 == nil {
			return arr, nil
		}
		return nil, fmt.Errorf("parse conv: %w", err)
	}
	return env.Results, nil
}

func (c *linkedInMCPClient) Close() error {
	if c.stdin != nil {
		c.stdin.Flush()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return nil
}
