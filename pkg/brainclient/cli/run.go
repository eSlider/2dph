// Package cli — CLI bin/brain/client.go: сервисный клиент read-контракта
// brain (P-9.5). Подкоманды search/get/stats/audit поверх pkg/brainclient;
// тесты cgo-free, входят в обычный go test ./... (пакет без build-тегов).
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/pkg/brainclient"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/integrii/flaggy"
)

const usage = `usage: bin/brain/client.go <search|get|stats|audit> [args] [--json] [--base URL] [--token T]
  search QUERY [--root facts|info] [-n N] [--as-of YYYY-MM-DD] [--no-web]
  get ID [--body]
  stats
  audit
--root facts применяет «гейт facts»: в выдаче только подтверждённые факты,
отклонённое помечается (not confirmed).`

// conn — общие флаги клиента (base/token/json) + транспорт.
type conn struct {
	base  string
	token string
	json  bool
}

func (c *conn) addFlags(p *flaggy.Parser) {
	p.String(&c.base, "", "base", "brain API base URL (default from config)")
	p.String(&c.token, "", "token", "Bearer token")
	p.Bool(&c.json, "", "json", "JSON output")
}

func (c *conn) client() *brainclient.Client {
	return brainclient.New(brainclient.Config{Base: c.base, Token: c.token})
}

func (c *conn) encode(w io.Writer, v any) int {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return b2i(enc.Encode(v))
}

func b2i(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

// Run — точка входа CLI (bin/brain/client.go вызывает её из main).
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	switch args[0] {
	case "search":
		return runSearch(args[1:], stdout, stderr)
	case "get":
		return runGet(args[1:], stdout, stderr)
	case "stats":
		return runStats(args[1:], stdout, stderr)
	case "audit":
		return runAudit(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "brain/client: unknown command %q\n%s\n", args[0], usage)
		return 2
	}
}

// defaultBase подставляет base из internal/config (host/port), если флаг не
// задан — как в bin/brain/read-contract.go.
func defaultBase(c *conn) string {
	if c.base != "" {
		return c.base
	}
	cfg, err := config.Load(context.Background())
	if err == nil {
		host := cfg.Host
		if host == "" {
			host = "127.0.0.1"
		}
		port := cfg.Port
		if port <= 0 {
			port = 8630
		}
		return "http://" + host + ":" + strconv.Itoa(port)
	}
	return "http://127.0.0.1:8630"
}

func runSearch(args []string, stdout, stderr io.Writer) int {
	var (
		c   conn
		opt brainclient.SearchOptions
		q   string
	)
	p := cli.New("brain-client-search")
	p.Description = "deduction search через read-контракт (facts → info → web)"
	c.addFlags(p)
	p.String(&opt.Root, "", "root", "facts or info (facts включает гейт)")
	p.Int(&opt.Limit, "n", "n", "max hits (1..100)")
	p.String(&opt.AsOf, "", "as-of", "keep facts active on YYYY-MM-DD (D24)")
	p.Bool(&opt.NoWeb, "", "no-web", "stay local")
	p.AddPositionalValue(&q, "query", 1, false, "search query")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	q = cli.Query(q, p.TrailingArguments)
	if q == "" {
		fmt.Fprintln(stderr, "brain/client: search: query required")
		return 2
	}
	c.base = defaultBase(&c)

	ctx := context.Background()
	cl := c.client()
	if opt.Root == "facts" {
		f, err := cl.Facts(ctx, q, opt)
		if err != nil {
			return fail(stderr, err)
		}
		if c.json {
			return c.encode(stdout, f)
		}
		printFactsHuman(stdout, f)
		return 0
	}
	resp, err := cl.Search(ctx, q, opt)
	if err != nil {
		return fail(stderr, err)
	}
	if c.json {
		return c.encode(stdout, resp)
	}
	printSearchHuman(stdout, resp)
	return 0
}

func printSearchHuman(w io.Writer, r *contract.SearchResponse) {
	fmt.Fprintf(w, "query: %s\n", r.Query)
	if r.RootFilter != "" {
		fmt.Fprintf(w, "root: %s\n", r.RootFilter)
	}
	if r.AsOf != "" {
		fmt.Fprintf(w, "as_of: %s\n", r.AsOf)
	}
	for _, h := range r.Results {
		mark := ""
		if !brainclient.Gate(h.Root, h.Confidence) {
			mark = "  " + brainclient.GateReason(h.Root, h.Confidence)
		}
		fmt.Fprintf(w, "- %s [%s/%s] %.2f — %s%s\n", h.ID, h.Root, h.Confidence, h.Score, firstLine(h.Text), mark)
	}
}

func printFactsHuman(w io.Writer, f *brainclient.Facts) {
	fmt.Fprintf(w, "facts (%d confirmed):\n", f.Count)
	for _, h := range f.Confirmed {
		fmt.Fprintf(w, "  - %s [%.2f] %s\n", h.ID, h.Score, firstLine(h.Text))
	}
	if len(f.NotConfirmed) > 0 {
		fmt.Fprintln(w, "(not confirmed):")
		for _, nf := range f.NotConfirmed {
			fmt.Fprintf(w, "  - %s — %s  (%s)\n", nf.ID, firstLine(nf.Text), nf.Reason)
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return s
}

func runGet(args []string, stdout, stderr io.Writer) int {
	var (
		c    conn
		body bool
		id   string
	)
	p := cli.New("brain-client-get")
	p.Description = "read one leaf by id через read-контракт"
	c.addFlags(p)
	p.Bool(&body, "", "body", "include full text")
	p.AddPositionalValue(&id, "id", 1, false, "leaf id")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	c.base = defaultBase(&c)
	if id == "" {
		fmt.Fprintln(stderr, "brain/client: get: id required")
		return 2
	}
	resp, err := c.client().Get(context.Background(), id, brainclient.GetOptions{Body: body})
	if err != nil {
		return fail(stderr, err)
	}
	if c.json {
		return c.encode(stdout, resp)
	}
	fmt.Fprintf(stdout, "id: %s\nroot: %s\nconfidence: %s\nsource: %s\ntype: %s\n",
		resp.ID, resp.Root, resp.Confidence, resp.Source, resp.Type)
	if resp.ValidFrom != "" {
		fmt.Fprintf(stdout, "valid_from: %s\n", resp.ValidFrom)
	}
	if resp.ValidTo != "" {
		fmt.Fprintf(stdout, "valid_to: %s\n", resp.ValidTo)
	}
	if body && resp.Text != "" {
		fmt.Fprintf(stdout, "text:\n%s\n", resp.Text)
	}
	return 0
}

func runStats(args []string, stdout, stderr io.Writer) int {
	var c conn
	p := cli.New("brain-client-stats")
	p.Description = "index health через read-контракт"
	c.addFlags(p)
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	c.base = defaultBase(&c)
	resp, err := c.client().Stats(context.Background())
	if err != nil {
		return fail(stderr, err)
	}
	if c.json {
		return c.encode(stdout, resp)
	}
	fmt.Fprintf(stdout, "total: %d\n", resp.Total)
	for root, n := range resp.ByRoot {
		fmt.Fprintf(stdout, "by_root %s: %d\n", root, n)
	}
	return 0
}

func runAudit(args []string, stdout, stderr io.Writer) int {
	var c conn
	p := cli.New("brain-client-audit")
	p.Description = "гистограмма root × confidence + гейт facts-корня"
	c.addFlags(p)
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	c.base = defaultBase(&c)
	resp, err := c.client().Audit(context.Background())
	if err != nil {
		return fail(stderr, err)
	}
	g := brainclient.GateAudit(resp)
	if c.json {
		if code := c.encode(stdout, map[string]any{
			"contract_version": resp.ContractVersion,
			"status":           resp.Status,
			"by_confidence":    resp.ByConfidence,
			"facts_gate":       g,
		}); code != 0 {
			return code
		}
	} else {
		for _, row := range resp.ByConfidence {
			fmt.Fprintf(stdout, "%-5s %-10s %d\n", row.Root, row.Confidence, row.Count)
		}
		fmt.Fprintf(stdout, "facts gate: confirmed=%d not_confirmed=%d\n", g.ConfirmedFacts, g.NotConfirmedFacts)
	}
	if !g.OK() {
		fmt.Fprintf(stderr, "brain/client: audit: на facts-корне есть не подтверждённый confidence "+
			"(%d листов) — не подавать как факты; разбор: brain/audit-contract\n", g.NotConfirmedFacts)
		return 1
	}
	return 0
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "brain/client: %v\n", err)
	return 1
}
