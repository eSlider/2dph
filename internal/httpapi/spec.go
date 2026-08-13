package httpapi

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
	{Path: PathIngest, Method: "get", ID: "ingest", Summary: "rebuild hint (write is v2)", MCP: true},
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
