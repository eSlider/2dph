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

type MCPClient struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdout *bufio.Scanner
	msgID  int
}

type mcpRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type mcpToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError,omitempty"`
}

type ListChatsResult struct {
	ChatID   int64  `json:"chat_id"`
	Title    string `json:"name"`
	Type     string `json:"type"`
	Username string `json:"username,omitempty"`
}

type listChatsEnvelope struct {
	Results []ListChatsResult `json:"results"`
}

type historyEnvelope struct {
	Results []GetHistoryResult `json:"results"`
}

type GetHistoryResult struct {
	ID     int    `json:"id"`
	Sender string `json:"sender"`
	Date   string `json:"date"`
	Text   string `json:"text"`
	Media  string `json:"media,omitempty"`
	Out    bool   `json:"out,omitempty"`
}

func NewMCPClient(ctx context.Context, apiID int, apiHash, phone, sessionString, mcpDir string) (*MCPClient, error) {
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("TELEGRAM_API_ID=%d", apiID),
		fmt.Sprintf("TELEGRAM_API_HASH=%s", apiHash),
		fmt.Sprintf("TELEGRAM_PHONE=%s", phone),
		fmt.Sprintf("TELEGRAM_SESSION_STRING=%s", sessionString),
		"MCP_TRANSPORT=stdio",
	)

	serverPath := filepath.Join(mcpDir, ".venv", "bin", "python3")
	mainPath := filepath.Join(mcpDir, "main.py")

	cmd := exec.CommandContext(ctx, serverPath, mainPath)
	cmd.Env = env
	cmd.Dir = mcpDir

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
		return nil, fmt.Errorf("start mcp: %w", err)
	}

	c := &MCPClient{
		cmd:    cmd,
		stdin:  bufio.NewWriter(stdin),
		stdout: bufio.NewScanner(stdout),
		msgID:  0,
	}
	c.stdout.Buffer(make([]byte, 1<<20), 1<<20)

	if err := c.initialize(ctx); err != nil {
		c.Close()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	return c, nil
}

func (c *MCPClient) nextID() int {
	c.msgID++
	return c.msgID
}

func (c *MCPClient) sendRequest(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID()
	req := mcpRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	if _, err := c.stdin.Write(body); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	if err := c.stdin.WriteByte('\n'); err != nil {
		return nil, fmt.Errorf("write newline: %w", err)
	}
	if err := c.stdin.Flush(); err != nil {
		return nil, fmt.Errorf("flush: %w", err)
	}

	for c.stdout.Scan() {
		line := c.stdout.Text()
		if line == "" {
			continue
		}

		var resp mcpResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w\nline: %s", err, line[:min(len(line), 500)])
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
	return nil, fmt.Errorf("no response: %w", c.stdout.Err())
}

func (c *MCPClient) initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]string{
			"name":    "chats-sync",
			"version": "0.1.0",
		},
	}
	_, err := c.sendRequest(ctx, "initialize", params)
	return err
}

func (c *MCPClient) ListChats(ctx context.Context, chatType string, limit int) ([]ListChatsResult, error) {
	args := map[string]interface{}{
		"chat_type": chatType,
		"limit":     limit,
	}
	result, err := c.sendRequest(ctx, "tools/call", map[string]interface{}{
		"name":      "list_chats",
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}

	var toolRes mcpToolResult
	if err := json.Unmarshal(result, &toolRes); err != nil {
		return nil, fmt.Errorf("unmarshal tool result: %w", err)
	}
	if toolRes.IsError {
		msg := "unknown"
		if len(toolRes.Content) > 0 {
			msg = toolRes.Content[0].Text
		}
		return nil, fmt.Errorf("list_chats error: %s", msg)
	}
	if len(toolRes.Content) == 0 {
		return nil, nil
	}

	text := toolRes.Content[0].Text
	if text == "" || text == "No chats found matching the criteria." {
		return nil, nil
	}

	var env listChatsEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		var arr []ListChatsResult
		if err2 := json.Unmarshal([]byte(text), &arr); err2 != nil {
			return nil, fmt.Errorf("parse chats: %w (also tried array: %v)\nbody: %s", err, err2, text[:min(len(text), 500)])
		}
		return arr, nil
	}
	return env.Results, nil
}

func (c *MCPClient) GetHistory(ctx context.Context, chatID int64, limit int) ([]GetHistoryResult, error) {
	args := map[string]interface{}{
		"chat_id": chatID,
		"limit":   limit,
	}
	result, err := c.sendRequest(ctx, "tools/call", map[string]interface{}{
		"name":      "get_history",
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}

	var toolRes mcpToolResult
	if err := json.Unmarshal(result, &toolRes); err != nil {
		return nil, fmt.Errorf("unmarshal tool result: %w", err)
	}
	if toolRes.IsError {
		msg := "unknown"
		if len(toolRes.Content) > 0 {
			msg = toolRes.Content[0].Text
		}
		return nil, fmt.Errorf("get_history error: %s", msg)
	}
	if len(toolRes.Content) == 0 {
		return nil, nil
	}

	text := toolRes.Content[0].Text
	if text == "" || text == "No messages found for this page." || text == "No messages found matching the criteria." {
		return nil, nil
	}

	var env historyEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		var arr []GetHistoryResult
		if err2 := json.Unmarshal([]byte(text), &arr); err2 != nil {
			return nil, fmt.Errorf("parse history: %w (also tried array: %v)\nbody: %s", err, err2, text[:min(len(text), 500)])
		}
		return arr, nil
	}
	return env.Results, nil
}

func (c *MCPClient) Close() error {
	if c.stdin != nil {
		c.stdin.Flush()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return nil
}

type TelegramMCPSource struct {
	mcpDir     string
	apiID      int
	apiHash    string
	phone      string
	sessionStr string
	limit      int
}

func NewTelegramMCPSource(apiID int, apiHash, phone, sessionString, mcpDir string) *TelegramMCPSource {
	return &TelegramMCPSource{
		mcpDir:     mcpDir,
		apiID:      apiID,
		apiHash:    apiHash,
		phone:      phone,
		sessionStr: sessionString,
	}
}

func (s *TelegramMCPSource) Name() string { return "telegram" }

func (s *TelegramMCPSource) Sync(ctx context.Context, outDir string, limit int) error {
	if limit > 0 {
		s.limit = limit
	}

	client, err := NewMCPClient(ctx, s.apiID, s.apiHash, s.phone, s.sessionStr, s.mcpDir)
	if err != nil {
		return fmt.Errorf("mcp client: %w", err)
	}
	defer client.Close()

	chats, err := client.ListChats(ctx, "user", 100)
	if err != nil {
		return fmt.Errorf("list chats: %w", err)
	}
	if len(chats) == 0 {
		fmt.Println("chats: no personal chats found")
		return nil
	}
	fmt.Printf("chats: found %d personal chats\n", len(chats))

	var filtered []ListChatsResult
	for _, c := range chats {
		if strings.Contains(strings.ToLower(c.Username), "bot") {
			continue
		}
		if c.ChatID == 777000 { // Telegram service
			continue
		}
		filtered = append(filtered, c)
	}
	fmt.Printf("chats: %d after filter (bots excluded)\n", len(filtered))

	for _, chat := range filtered {
		chatID := fmt.Sprintf("user_%d", chat.ChatID)
		chatName := chat.Title
		if chatName == "" {
			chatName = chatID
		}

		chatDir := filepath.Join(outDir, "telegram", chatID)
		if err := os.MkdirAll(chatDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "chats: mkdir %s: %v\n", chatDir, err)
			continue
		}

		jsonlPath := filepath.Join(chatDir, "messages.jsonl")
		f, err := os.Create(jsonlPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chats: create %s: %v\n", jsonlPath, err)
			continue
		}

		msgLimit := 100
		if s.limit > 0 {
			msgLimit = s.limit
		}

		msgs, err := client.GetHistory(ctx, chat.ChatID, msgLimit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chats: get_history for %s: %v\n", chatName, err)
			f.Close()
			continue
		}

		enc := json.NewEncoder(f)
		written := 0
		for _, m := range msgs {
			if m.Out {
				continue
			}
			text := m.Text
			if text == "" && m.Media != "" {
				text = fmt.Sprintf("[%s]", m.Media)
			}
			if text == "" {
				continue
			}

			sender := cleanSender(m.Sender)
			ts := m.Date
			if t, err := time.Parse(time.RFC3339, m.Date); err == nil {
				ts = t.UTC().Format(time.RFC3339)
			}

			chatMsg := Message{
				ID:        fmt.Sprintf("tg_%d_%d", chat.ChatID, m.ID),
				Timestamp: ts,
				From:      sender,
				Text:      text,
				Platform:  "telegram",
			}
			if m.Media != "" {
				desc := fmt.Sprintf("[%s]", m.Media)
				chatMsg.Media = &desc
			}
			if err := enc.Encode(chatMsg); err != nil {
				fmt.Fprintf(os.Stderr, "chats: encode msg: %v\n", err)
				continue
			}
			written++
		}
		f.Close()

		if written > 0 {
			fmt.Printf("chats: synced %s (%s) — %d messages\n", chatName, chatID, written)
		}
	}

	return nil
}

func cleanSender(sender string) string {
	if idx := strings.Index(sender, " ("); idx > 0 {
		sender = sender[:idx]
	} else if idx := strings.Index(sender, " @"); idx > 0 {
		sender = sender[:idx]
	}
	if idx := strings.Index(sender, " ["); idx > 0 {
		sender = sender[:idx]
	}
	return sender
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
