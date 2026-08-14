package websearch

import (
	"fmt"

	"github.com/eSlider/2dph/internal/cli"
	"github.com/integrii/flaggy"
)

type CLI struct {
	Query, Site, Lang, Fresh, Category, Engines string
	Limit                                       int
	JSONOut, Refresh, Force                     bool
	TTL                                         float64
	Timeout                                     int
}

func NewCLI() CLI {
	return CLI{Limit: DefaultLimit, TTL: float64(CacheTTL), Timeout: 25}
}

func Parser() *flaggy.Parser {
	c := NewCLI()
	return Bind(&c)
}

func Bind(c *CLI) *flaggy.Parser {
	p := cli.New("web-search")
	p.Description = "SearXNG second source (throttled ≠ absence)"
	p.Bool(&c.JSONOut, "", "json", "JSON output")
	p.Bool(&c.Refresh, "", "refresh", "bypass cache")
	p.Bool(&c.Force, "", "force", "allow PII in query")
	p.Int(&c.Limit, "n", "limit", "max hits")
	p.String(&c.Site, "", "site", "restrict to host")
	p.String(&c.Lang, "", "lang", "language")
	p.String(&c.Fresh, "", "fresh", "day|week|month|year")
	p.String(&c.Category, "", "category", "searx category")
	p.String(&c.Engines, "", "engines", "engine list")
	p.Float64(&c.TTL, "", "ttl", "cache ttl seconds")
	p.Int(&c.Timeout, "", "timeout", "http timeout seconds")
	return p
}

func ParseArgs(args []string) (CLI, error) {
	c := NewCLI()
	p := Bind(&c)
	var q string
	p.AddPositionalValue(&q, "query", 1, false, "search query")
	if err := cli.Parse(p, args); err != nil {
		return c, err
	}
	c.Query = cli.Query(q, p.TrailingArguments)
	if c.Query == "" {
		return c, fmt.Errorf("query required")
	}
	if c.Limit < 0 {
		return c, fmt.Errorf("--limit must be a non-negative integer")
	}
	if c.Timeout <= 0 {
		return c, fmt.Errorf("--timeout must be a positive integer")
	}
	return c, nil
}
