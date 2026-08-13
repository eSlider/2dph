// Command line parsing for kbsearch. Kept free of cgo/db imports so the
// parser is unit-testable on its own.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

const usage = `usage: kbsearch "query" [--root facts|info] [--repo REPO] [-n N] [--hop N] [--json]
       kbsearch serve [port]
       kbsearch --list-model`

type options struct {
	query     string
	root      string
	repo      string
	limit     int
	hops      int
	jsonOut   bool
	listModel bool
}

// parseArgs reads the flags. Unknown flags are an error: silently dropping
// them meant `--hop 1` vanished and its argument `1` was appended to the
// query, so the search quietly answered a different question.
func parseArgs(args []string) (options, error) {
	opt := options{limit: 20}
	var queryArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		wantsValue := arg == "--root" || arg == "--repo" || arg == "-n" || arg == "--hop"
		if wantsValue && i+1 >= len(args) {
			return opt, fmt.Errorf("%s needs a value", arg)
		}
		switch arg {
		case "--root":
			i++
			opt.root = args[i]
			if opt.root != "facts" && opt.root != "info" {
				return opt, fmt.Errorf("--root must be facts or info, got %q", opt.root)
			}
		case "--repo":
			i++
			opt.repo = args[i]
		case "-n":
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				return opt, fmt.Errorf("-n must be a positive integer, got %q", args[i])
			}
			opt.limit = n
		case "--hop":
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				return opt, fmt.Errorf("--hop must be a non-negative integer, got %q", args[i])
			}
			opt.hops = n
		case "--json":
			opt.jsonOut = true
		case "--list-model":
			opt.listModel = true
		default:
			if strings.HasPrefix(arg, "-") {
				return opt, fmt.Errorf("unknown flag %q", arg)
			}
			queryArgs = append(queryArgs, arg)
		}
	}

	opt.query = strings.TrimSpace(strings.Join(queryArgs, " "))
	if opt.query == "" && !opt.listModel {
		return opt, fmt.Errorf("no query given")
	}
	return opt, nil
}
