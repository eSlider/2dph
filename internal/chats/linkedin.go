package chats

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// mcp-server-linkedin v4.22 returns get_inbox / get_conversation as
// {url, sections:{inbox|conversation: textblob}, references:{...}}.
// The conversation list lives in references (kind=conversation); messages live
// in the sections text blob, delimited by "<From> sent the following message
// at <time>" markers. See testdata/linkedin_*.json for the wire shape.

type lnEnvelope struct {
	URL        string          `json:"url"`
	Sections   map[string]any  `json:"sections"`
	References map[string]any  `json:"references"`
}

type lnReference struct {
	Kind    string `json:"kind"`
	URL     string `json:"url"`
	Text    string `json:"text"`
	Context string `json:"context"`
}

type lnMessage struct {
	From string `json:"from"`
	Date string `json:"date"`
	Text string `json:"text"`
}

var (
	lnWeekdays = map[string]time.Weekday{
		"SUNDAY": time.Sunday, "MONDAY": time.Monday, "TUESDAY": time.Tuesday,
		"WEDNESDAY": time.Wednesday, "THURSDAY": time.Thursday,
		"FRIDAY": time.Friday, "SATURDAY": time.Saturday,
	}
	lnMsgStartRe = regexp.MustCompile(`^(.+?) sent the following messages? at (.+)$`)
	lnTimeRe     = regexp.MustCompile(`\d{1,2}:\d{2}\s*[AP]M`)
)

func isWeekdayLine(s string) bool {
	if _, ok := lnWeekdays[s]; ok {
		return true
	}
	switch s {
	case "TODAY", "YESTERDAY", "THIS WEEK", "LAST WEEK":
		return true
	}
	return lnMonthDayRe.MatchString(s)
}

var lnMonthDayRe = regexp.MustCompile(`^[A-Z]{3}\s+\d{1,2}$`)

// parseLinkedInInbox extracts conversations from a get_inbox response.
func parseLinkedInInbox(text string) []lnInboxItem {
	var env lnEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		return nil
	}
	refs, _ := env.References["inbox"].([]any)
	var items []lnInboxItem
	for _, r := range refs {
		rr, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if rr["kind"] != "conversation" {
			continue
		}
		u, _ := rr["url"].(string)
		tid := threadIDFromURL(u)
		if !validThreadID(tid) {
			continue
		}
		name, _ := rr["text"].(string)
		items = append(items, lnInboxItem{
			ThreadID:     tid,
			Participants: name,
		})
	}
	return items
}

// parseLinkedInConversation parses the sections.conversation text blob into
// messages. Messages are delimited by "<From> sent the following message(s) at
// <time>" lines; each message body runs until the next marker. Day headers
// (all-caps weekdays) provide date context; times are mapped to the most
// recent matching weekday.
func parseLinkedInConversation(text string) []lnMessage {
	var env lnEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		return nil
	}
	blob, _ := env.Sections["conversation"].(string)
	if blob == "" {
		return nil
	}

	var msgs []lnMessage
	var cur *lnMessage
	var body []string
	day := ""

	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(strings.Join(body, "\n"))
		if ts := linkedInTimestamp(day, cur.Date); ts != "" {
			cur.Date = ts
		}
		if cur.Text != "" {
			msgs = append(msgs, *cur)
		}
		cur = nil
		body = nil
	}

	for _, raw := range strings.Split(blob, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if isWeekdayLine(line) {
			if line != day {
				// A new day header terminates the previous message,
				// which must keep the earlier date context.
				flush()
			}
			day = line
			continue
		}
		if m := lnMsgStartRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = &lnMessage{From: strings.TrimSpace(m[1]), Date: strings.TrimSpace(m[2])}
			continue
		}
		if cur == nil {
			continue
		}
		// Skip "View X's profile" and the "<From> (pronouns) <time>" header.
		if strings.HasPrefix(line, "View ") && strings.HasSuffix(line, "'s profile") {
			continue
		}
		if strings.HasPrefix(line, cur.From) && lnTimeRe.MatchString(line) {
			continue
		}
		body = append(body, line)
	}
	flush()
	return msgs
}

// linkedInTimestamp maps a weekday, relative, or MON DD date header + clock
// string to a timestamp, or returns "" when the clock cannot be parsed.
func linkedInTimestamp(day, clock string) string {
	t, err := time.Parse("3:04 PM", clock)
	if err != nil {
		return ""
	}
	now := time.Now()
	var d time.Time
	if wd, ok := lnWeekdays[day]; ok {
		diff := (int(now.Weekday()) - int(wd) + 7) % 7
		d = now.AddDate(0, 0, -diff)
	} else {
		switch day {
		case "TODAY":
			d = now
		case "YESTERDAY":
			d = now.AddDate(0, 0, -1)
		case "THIS WEEK":
			diff := int(now.Weekday())
			d = now.AddDate(0, 0, -diff)
		case "LAST WEEK":
			diff := int(now.Weekday()) + 7
			d = now.AddDate(0, 0, -diff)
		default:
			if m := lnMonthDayRe.FindStringSubmatch(day); m != nil {
				// MON DD without a year: resolve to the most recent
				// occurrence that is not in the future.
				d = monthDayDate(day, now)
				if d.IsZero() {
					return t.Format("15:04")
				}
			} else {
				// No date context; keep bare clock time.
				return t.Format("15:04")
			}
		}
	}
	res := time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
	return res.UTC().Format(time.RFC3339)
}

var lnMonths = map[string]time.Month{
	"JAN": time.January, "FEB": time.February, "MAR": time.March,
	"APR": time.April, "MAY": time.May, "JUN": time.June,
	"JUL": time.July, "AUG": time.August, "SEP": time.September,
	"OCT": time.October, "NOV": time.November, "DEC": time.December,
}

// monthDayDate resolves "MON DD" to the most recent occurrence of that date,
// preferring the current year and falling back to the previous year when the
// date is in the future. Returns zero time when unresolvable.
func monthDayDate(day string, now time.Time) time.Time {
	parts := strings.Fields(day)
	if len(parts) != 2 {
		return time.Time{}
	}
	mo, ok := lnMonths[parts[0]]
	if !ok {
		return time.Time{}
	}
	var dd int
	if _, err := fmt.Sscanf(parts[1], "%d", &dd); err != nil {
		return time.Time{}
	}
	if dd < 1 || dd > 31 {
		return time.Time{}
	}
	d := time.Date(now.Year(), mo, dd, 0, 0, 0, 0, time.UTC)
	if d.After(now) {
		d = d.AddDate(-1, 0, 0)
	}
	if d.After(now) {
		return time.Time{}
	}
	return d
}

func threadIDFromURL(u string) string {
	u = strings.TrimSuffix(u, "/")
	idx := strings.LastIndex(u, "/")
	if idx < 0 {
		return ""
	}
	return u[idx+1:]
}

// validThreadID rejects path segments that are not real thread ids (e.g. the
// literal "thread" or an empty trailing segment).
func validThreadID(id string) bool {
	if id == "" || id == "thread" {
		return false
	}
	return true
}

func NewLinkedInMCPSource(userDataDir string) *LinkedInMCPSource {
	return &LinkedInMCPSource{userDataDir: userDataDir}
}

func (s *LinkedInMCPSource) Name() string { return "linkedin" }

func (s *LinkedInMCPSource) Sync(ctx context.Context, outDir string, limit int) error {
	if limit > 0 {
		s.limit = limit
	}

	// getConversation fetches one thread, recreating the MCP server when it
	// wedges. A single 429 makes mcp-server-linkedin close its browser and
	// refuse every later call ("still has a browser open"), so a broken server
	// must be restarted rather than hammered.
	getConversation := func(threadID string) ([]lnMessage, error) {
		client, err := newLinkedInMCP(ctx, s.userDataDir)
		if err != nil {
			return nil, fmt.Errorf("linkedin mcp: %w", err)
		}
		defer client.Close()
		msgs, err := client.GetConversation(ctx, "", threadID, msgLimitFor(s.limit))
		if err != nil && wedged(err) {
			fmt.Fprintf(os.Stderr, "chats: %s: server wedged, restarting broker\n", threadID)
			time.Sleep(5 * time.Second)
			client2, cerr := newLinkedInMCP(ctx, s.userDataDir)
			if cerr == nil {
				defer client2.Close()
				msgs, err = client2.GetConversation(ctx, "", threadID, msgLimitFor(s.limit))
			}
		}
		return msgs, err
	}

	client, err := newLinkedInMCP(ctx, s.userDataDir)
	if err != nil {
		return fmt.Errorf("linkedin mcp: %w", err)
	}
	inbox, err := client.GetInbox(ctx, 50)
	if err != nil {
		client.Close()
		return fmt.Errorf("get_inbox: %w", err)
	}
	client.Close()
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

		msgs, err := getConversation(conv.ThreadID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chats: get_conversation %s: %v\n", convID, err)
			continue
		}

		jsonlPath := filepath.Join(chatDir, "messages.jsonl")

		// A rate-limited response can parse to zero messages. Never clobber
		// previously synced data with an empty file.
		if len(msgs) == 0 {
			fmt.Fprintf(os.Stderr, "chats: %s (%s): 0 messages parsed, keeping existing file\n", chatName, convID)
			continue
		}

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

		// Pause between conversations to reduce LinkedIn rate limiting.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
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
		"--no-daemon",
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

// msgLimitFor returns the per-conversation message cap for a sync.
func msgLimitFor(limit int) int {
	if limit > 0 {
		return limit
	}
	return 100
}

// wedged reports whether a conversation fetch failure means the MCP server
// closed its browser and will refuse every later call.
func wedged(err error) bool {
	return strings.Contains(err.Error(), "still has a browser open")
}

// callTool invokes an MCP tool, retrying transient (rate-limit) failures.
func (c *linkedInMCPClient) callTool(ctx context.Context, name string, params map[string]interface{}) (json.RawMessage, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt)) * 5 * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		result, err := c.send(ctx, "tools/call", map[string]interface{}{
			"name":      name,
			"arguments": params,
		})
		if err == nil {
			// Tool-level errors surface as a successful RPC with an
			// isError=true content entry.
			if hint := toolErrorHint(result); hint != "" {
				lastErr = fmt.Errorf("%s error: %s", name, hint)
				if !isTransientLinkedInError(lastErr.Error()) {
					return nil, lastErr
				}
				continue
			}
			return result, nil
		}
		lastErr = err
		if !isTransientLinkedInError(err.Error()) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%s: %w", name, lastErr)
}

// toolErrorHint returns the tool's error text when the result has isError set.
func toolErrorHint(result json.RawMessage) string {
	var toolRes struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &toolRes); err != nil || !toolRes.IsError {
		return ""
	}
	if len(toolRes.Content) > 0 {
		return toolRes.Content[0].Text
	}
	return "unknown tool error"
}

// isTransientLinkedInError reports whether a fetch failed due to rate limiting
// or a transient server error, which may succeed on retry.
func isTransientLinkedInError(msg string) bool {
	return strings.Contains(msg, "503") || strings.Contains(msg, "429") ||
		strings.Contains(msg, "ERR_HTTP_RESPONSE_CODE_FAILURE") ||
		strings.Contains(msg, "ERR_ABORTED") ||
		strings.Contains(msg, "Error calling tool") ||
		strings.Contains(msg, "Unexpected error") ||
		strings.Contains(msg, "still has a browser open")
}

func (c *linkedInMCPClient) GetInbox(ctx context.Context, limit int) ([]lnInboxItem, error) {
	params := map[string]interface{}{
		"limit": limit,
	}
	result, err := c.callTool(ctx, "get_inbox", params)
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
	if len(toolRes.Content) == 0 {
		return nil, nil
	}

	text := toolRes.Content[0].Text
	return parseLinkedInInbox(text), nil
}

func (c *linkedInMCPClient) GetConversation(ctx context.Context, username, threadID string, limit int) ([]lnMessage, error) {
	params := map[string]interface{}{
		"linkedin_username": username,
		"thread_id":         threadID,
		"index":             limit,
	}
	result, err := c.callTool(ctx, "get_conversation", params)
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
	if len(toolRes.Content) == 0 {
		return nil, nil
	}

	text := toolRes.Content[0].Text
	return parseLinkedInConversation(text), nil
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
