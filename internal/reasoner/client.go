package reasoner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HF IDs named in docs. No Qwen3.6-9B exists.
const (
	HFQwen35_9B   = "Qwen/Qwen3.5-9B"
	HFQwen36_27B  = "Qwen/Qwen3.6-27B"
	HFBonsai27B   = "prism-ml/Bonsai-27B-gguf"
	OllamaRAM     = "qwen3.5:9b"
	OllamaQuality = "MichelRosselli/bonsai-27b:Q1_0"
)

type ToolCall struct {
	Name      string
	Arguments string
}

type Result struct {
	Model      string `json:"model"`
	OK         bool   `json:"ok"`
	ToolName   string `json:"tool_name,omitempty"`
	XMLLeak    bool   `json:"xml_leak"`
	Err        string `json:"error,omitempty"`
	LatencyMS  int64  `json:"latency_ms"`
	RSSMB      int    `json:"rss_mb,omitempty"`
	Device     string `json:"device"`
	WantedTool string `json:"wanted_tool"`
}

type Prompt struct {
	Name string
	Want string
	User string
}

var BakePrompts = []Prompt{
	{
		Name: "search-before-claim",
		Want: "search",
		User: "Use tools. Search the 2dph brain for LadybugDB before you answer. Call search.",
	},
	{
		Name: "get-leaf",
		Want: "get",
		User: "Use tools. Fetch leaf id leaf-demo with get. Do not invent the body.",
	},
	{
		Name: "audit-index",
		Want: "audit",
		User: "Use tools. Call audit on the brain index health.",
	},
}

func MCPTools() []map[string]any {
	return []map[string]any{
		openaiTool("search", "deduction search (facts → info → web)", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"q": map[string]any{"type": "string", "description": "search query"},
				"n": map[string]any{"type": "integer"},
			},
			"required": []string{"q"},
		}),
		openaiTool("get", "read one leaf by id", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":   map[string]any{"type": "string"},
				"body": map[string]any{"type": "boolean"},
			},
			"required": []string{"id"},
		}),
		openaiTool("audit", "facts confidence histogram", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}),
	}
}

func openaiTool(name, desc string, schema map[string]any) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": desc,
			"parameters":  schema,
		},
	}
}

type Client struct {
	BaseURL    string
	Model      string
	HTTP       *http.Client
	Device     string
	ToolChoice string
}

type Report struct {
	Model        string   `json:"model"`
	HF           string   `json:"hf_id"`
	Device       string   `json:"device"`
	ToolCallOK   int      `json:"tool_call_ok"`
	ToolCallN    int      `json:"tool_call_n"`
	XMLLeak      int      `json:"xml_leak"`
	RSSMB        int      `json:"rss_mb"`
	VRAMMB       int      `json:"vram_mb"`
	LatencyP50MS float64  `json:"latency_p50_ms,omitempty"`
	LatencyP95MS float64  `json:"latency_p95_ms,omitempty"`
	Prompts      []Result `json:"prompts"`
}

func HFFor(model string) string {
	switch model {
	case OllamaRAM:
		return HFQwen35_9B
	case OllamaQuality:
		return HFBonsai27B
	default:
		if strings.Contains(model, "qwen3.6") || strings.Contains(model, "Qwen3.6") {
			return HFQwen36_27B
		}
		return ""
	}
}

func (c *Client) httpc() *http.Client {
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 10 * time.Minute}
	}
	return c.HTTP
}

func Origin(base string) string {
	s := strings.TrimRight(base, "/")
	return strings.TrimSuffix(s, "/v1")
}

func (c Client) ChatTools(user string) (ToolCall, string, error) {
	choice := c.ToolChoice
	if choice == "" {
		choice = "required"
	}
	body, _ := json.Marshal(map[string]any{
		"model": c.Model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are PicoClaw talking to 2dph MCP. Always call a tool before a factual claim. search then get then audit."},
			{"role": "user", "content": user},
		},
		"tools":       MCPTools(),
		"tool_choice": choice,
	})
	base := strings.TrimRight(c.BaseURL, "/")
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	url := base + "/chat/completions"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ToolCall{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.httpc().Do(req)
	if err != nil {
		return ToolCall{}, "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return ToolCall{}, string(raw), fmt.Errorf("http %d", res.StatusCode)
	}
	return ParseToolResponse(raw)
}

func ParseToolResponse(raw []byte) (ToolCall, string, error) {
	var wrap struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return ToolCall{}, "", err
	}
	content := ""
	if len(wrap.Choices) > 0 {
		content = wrap.Choices[0].Message.Content
		if n := len(wrap.Choices[0].Message.ToolCalls); n > 0 {
			fn := wrap.Choices[0].Message.ToolCalls[0].Function
			return ToolCall{Name: fn.Name, Arguments: rawArgs(fn.Arguments)}, content, nil
		}
	}
	return ToolCall{}, content, fmt.Errorf("no tool_calls")
}

func rawArgs(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func XMLLeak(content string) bool {
	s := strings.ToLower(content)
	return strings.Contains(s, "<tool_call>") ||
		strings.Contains(s, "<function=") ||
		strings.Contains(s, "<parameter")
}

func RunPrompt(c Client, p Prompt) Result {
	start := time.Now()
	tc, content, err := c.ChatTools(p.User)
	out := Result{
		Model:      c.Model,
		WantedTool: p.Want,
		LatencyMS:  time.Since(start).Milliseconds(),
		Device:     c.Device,
		XMLLeak:    XMLLeak(content),
	}
	if err != nil {
		out.Err = err.Error()
		if content != "" && out.XMLLeak {
			out.Err = "xml tool call instead of openai tool_calls"
		}
		return out
	}
	out.ToolName = tc.Name
	out.OK = tc.Name == p.Want
	if !out.OK {
		out.Err = "wanted " + p.Want + " got " + tc.Name
	}
	return out
}

type ProcMem struct {
	Name   string
	SizeMB int
	VRAMMB int
}

func ParsePS(raw []byte) []ProcMem {
	var wrap struct {
		Models []struct {
			Name     string `json:"name"`
			Size     int64  `json:"size"`
			SizeVRAM int64  `json:"size_vram"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil
	}
	out := make([]ProcMem, 0, len(wrap.Models))
	for _, m := range wrap.Models {
		out = append(out, ProcMem{
			Name:   m.Name,
			SizeMB: int(m.Size / (1024 * 1024)),
			VRAMMB: int(m.SizeVRAM / (1024 * 1024)),
		})
	}
	return out
}

func (c Client) FetchPS() []ProcMem {
	url := Origin(c.BaseURL) + "/api/ps"
	res, err := c.httpc().Get(url)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return nil
	}
	return ParsePS(raw)
}

func Run(c Client) Report {
	if c.Device == "" {
		c.Device = "cpu"
	}
	rep := Report{
		Model:  c.Model,
		HF:     HFFor(c.Model),
		Device: c.Device,
	}
	for _, p := range BakePrompts {
		r := RunPrompt(c, p)
		rep.Prompts = append(rep.Prompts, r)
		rep.ToolCallN++
		if r.OK {
			rep.ToolCallOK++
		}
		if r.XMLLeak {
			rep.XMLLeak++
		}
	}
	if mems := c.FetchPS(); len(mems) > 0 {
		rep.RSSMB = mems[0].SizeMB
		rep.VRAMMB = mems[0].VRAMMB
		if c.Device == "cpu" && mems[0].VRAMMB > 0 {
			rep.Device = "gpu"
		}
		for i := range rep.Prompts {
			rep.Prompts[i].RSSMB = rep.RSSMB
		}
	}
	return rep
}
