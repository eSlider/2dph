package rank

import (
	"fmt"
	"strconv"

	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/internal/facts"
	"github.com/integrii/flaggy"
)

const Usage = `usage: bin/brain/search.go "query" [--root facts|info] [--repo REPO] [-n N] [--hop N] [--as-of YYYY-MM-DD] [--sort date[:asc|:desc]] [--json] [--no-web]
       bin/brain/search.go serve [port]
       bin/brain/search.go --list-model
       source <(./bin/shell/complete.go bash)`

type Options struct {
	Query     string
	Root      string
	Repo      string
	Limit     int
	Hop       int
	AsOf      string
	Sort      string
	SortDate  bool
	SortDesc  bool
	JSONOut   bool
	ListModel bool
	NoWeb     bool
}

// NewParser is the flaggy schema for search (also used by bin/shell/complete.go).
func NewParser(opt *Options) *flaggy.Parser {
	if opt.Limit == 0 {
		opt.Limit = 20
	}
	p := cli.New("brain-search")
	p.Description = "deduction search: facts → info → web"
	p.String(&opt.Root, "", "root", "facts or info")
	p.String(&opt.Repo, "", "repo", "filter by repo")
	p.Int(&opt.Limit, "n", "n", "max hits")
	p.Int(&opt.Hop, "", "hop", "walk FROM_FILE depth 1-3")
	p.String(&opt.AsOf, "", "as-of", "keep facts active on YYYY-MM-DD (D24)")
	p.String(&opt.Sort, "", "sort", "order by date (date, date:asc, date:desc)")
	p.Bool(&opt.JSONOut, "", "json", "JSON output")
	p.Bool(&opt.NoWeb, "", "no-web", "stay local")
	p.Bool(&opt.ListModel, "", "list-model", "print embedding model")
	return p
}

// ParseArgs reads flags. Unknown flags are an error: silently dropping them
// meant `--hop 1` vanished and its argument `1` was appended to the query.
func ParseArgs(args []string) (Options, error) {
	opt := Options{Limit: 20}
	p := NewParser(&opt)
	var q string
	p.AddPositionalValue(&q, "query", 1, false, "search query")
	if err := cli.Parse(p, args); err != nil {
		return opt, err
	}
	opt.Query = cli.Query(q, p.TrailingArguments)
	if opt.Root != "" && opt.Root != "facts" && opt.Root != "info" {
		return opt, fmt.Errorf("--root must be facts or info, got %q", opt.Root)
	}
	if opt.Limit < 1 {
		return opt, fmt.Errorf("-n must be a positive integer, got %q", strconv.Itoa(opt.Limit))
	}
	if opt.Hop < 0 {
		return opt, fmt.Errorf("--hop must be a positive integer, got %q", strconv.Itoa(opt.Hop))
	}
	if opt.Hop > 3 {
		return opt, fmt.Errorf("--hop max is 3 (File → Commit → Person)")
	}
	if opt.AsOf != "" {
		day := facts.NormalizeDay(opt.AsOf)
		if len(day) != 10 || day[4] != '-' || day[7] != '-' {
			return opt, fmt.Errorf("--as-of must be YYYY-MM-DD, got %q", opt.AsOf)
		}
		opt.AsOf = day
	}
	if opt.Sort != "" {
		switch opt.Sort {
		case "date", "date:asc":
			opt.SortDate, opt.SortDesc = true, false
		case "date:desc":
			opt.SortDate, opt.SortDesc = true, true
		default:
			return opt, fmt.Errorf("--sort must be date, date:asc or date:desc, got %q", opt.Sort)
		}
	}
	if opt.Query == "" && !opt.ListModel {
		return opt, fmt.Errorf("no query given")
	}
	return opt, nil
}

// Parser is the search schema for bin/shell/complete.go.
func Parser() *flaggy.Parser {
	opt := Options{Limit: 20}
	p := NewParser(&opt)
	var q string
	p.AddPositionalValue(&q, "query", 1, false, "search query")
	return p
}
