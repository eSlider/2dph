package httpapi

import "strings"

// Shared HTTP surface: OpenAPI paths and MCP tools are generated from Ops.
// ServeHTTP must keep the same path strings.

type Param struct {
	Name, In, Type, Description string
	Required                    bool
}

type Op struct {
	Path, Method, ID, Summary string
	Params                    []Param
	MCP                       bool
}

const (
	PathHealth  = "/health"
	PathSearch  = "/search"
	PathGet     = "/get"
	PathStats   = "/stats"
	PathAudit   = "/audit"
	PathIngest  = "/ingest"
	PathOpenAPI = "/openapi.json"
	PathMCP     = "/mcp"
)

var Ops = []Op{
	{Path: PathHealth, Method: "get", ID: "health", Summary: "liveness"},
	{
		Path: PathSearch, Method: "get", ID: "search", Summary: "deduction search (facts → info → web)",
		MCP: true,
		Params: []Param{
			{Name: "q", In: "query", Type: "string", Description: "search query", Required: true},
			{Name: "n", In: "query", Type: "integer", Description: "hit limit 1..100 (default 10)"},
			{Name: "root", In: "query", Type: "string", Description: "facts or info (default: all roots)"},
			{Name: "noweb", In: "query", Type: "boolean", Description: "skip the web second source (D17)"},
			{Name: "as_of", In: "query", Type: "string", Description: "YYYY-MM-DD; keep facts active on that day (D24)"},
			{Name: "sort", In: "query", Type: "string", Description: "order by date (date, date:asc, date:desc)"},
		},
	},
	{
		Path: PathGet, Method: "get", ID: "get", Summary: "read one leaf by id",
		MCP: true,
		Params: []Param{
			{Name: "id", In: "query", Type: "string", Description: "leaf id", Required: true},
			{Name: "body", In: "query", Type: "boolean", Description: "include full text"},
		},
	},
	{Path: PathStats, Method: "get", ID: "stats", Summary: "index health", MCP: true},
	{Path: PathAudit, Method: "get", ID: "audit", Summary: "facts confidence histogram", MCP: true},
	{
		Path: PathIngest, Method: "post", ID: "ingest", Summary: "add a leaf without rebuild",
		MCP: true,
		Params: []Param{
			{Name: "text", In: "query", Type: "string", Description: "leaf text (omit for CLI hint)"},
			{Name: "root", In: "query", Type: "string", Description: "facts or info (default info)"},
			{Name: "source", In: "query", Type: "string", Description: "evidence pointer; facts need two sources"},
			{Name: "valid_from", In: "query", Type: "string", Description: "fact interval start YYYY-MM-DD (D24)"},
			{Name: "valid_to", In: "query", Type: "string", Description: "fact interval end YYYY-MM-DD inclusive (D24)"},
		},
	},
	{Path: PathOpenAPI, Method: "get", ID: "openapi", Summary: "OpenAPI 3 document for this server"},
}

func OpenAPI() map[string]any {
	paths := map[string]any{}
	for _, op := range Ops {
		params := make([]any, 0, len(op.Params))
		for _, p := range op.Params {
			params = append(params, map[string]any{
				"name":        p.Name,
				"in":          p.In,
				"required":    p.Required,
				"description": p.Description,
				"schema":      map[string]any{"type": p.Type},
			})
		}
		item := map[string]any{
			"operationId": op.ID,
			"summary":     op.Summary,
			"responses": map[string]any{
				"200": map[string]any{
					"description": "JSON",
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": map[string]any{"type": "object"},
						},
					},
				},
			},
		}
		if len(params) > 0 {
			item["parameters"] = params
		}
		paths[op.Path] = map[string]any{op.Method: item}
	}
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "2dph brain",
			"version":     "1",
			"description": "Same handlers as bin/brain/serve.go. MCP tools at POST /mcp match these paths.",
		},
		"paths": paths,
	}
}

type MCPTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func MCPTools() []MCPTool {
	out := make([]MCPTool, 0, len(Ops))
	for _, op := range Ops {
		if !op.MCP {
			continue
		}
		props := map[string]any{}
		var required []string
		for _, p := range op.Params {
			props[p.Name] = map[string]any{"type": p.Type, "description": p.Description}
			if p.Required {
				required = append(required, p.Name)
			}
		}
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		out = append(out, MCPTool{
			Name:        op.ID,
			Description: op.Summary,
			InputSchema: schema,
		})
	}
	return out
}

// SkillMarkdown is the Cursor skill fragment generated from Ops/MCPTools.
func SkillMarkdown() string {
	var b strings.Builder
	b.WriteString("# brain HTTP / MCP tools\n\n")
	b.WriteString("Generated from `internal/httpapi.Ops`. Do not edit by hand.\n\n")
	b.WriteString("Serve: `bin/brain/serve.go` (`GET /openapi.json`, `POST /mcp`).\n\n")
	for _, t := range MCPTools() {
		b.WriteString("- `")
		b.WriteString(t.Name)
		b.WriteString("` — ")
		b.WriteString(t.Description)
		b.WriteString("\n")
	}
	return b.String()
}
