package rank

import (
	"fmt"
	"strconv"
	"strings"
)

const Usage = `usage: bin/brain/search.go "query" [--root facts|info] [--repo REPO] [-n N] [--json] [--no-web]
       bin/brain/search.go serve [port]
       bin/brain/search.go --list-model`

type Options struct {
	Query     string
	Root      string
	Repo      string
	Limit     int
	JSONOut   bool
	ListModel bool
	NoWeb     bool
}

// ParseArgs reads flags. Unknown flags are an error: silently dropping them
// meant `--hop 1` vanished and its argument `1` was appended to the query.
// --hop is recognised so it cannot be swallowed; it is not implemented until
// File/FROM_FILE edges exist.
func ParseArgs(args []string) (Options, error) {
	opt := Options{Limit: 20}
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
			opt.Root = args[i]
			if opt.Root != "facts" && opt.Root != "info" {
				return opt, fmt.Errorf("--root must be facts or info, got %q", opt.Root)
			}
		case "--repo":
			i++
			opt.Repo = args[i]
		case "-n":
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				return opt, fmt.Errorf("-n must be a positive integer, got %q", args[i])
			}
			opt.Limit = n
		case "--hop":
			return opt, fmt.Errorf("--hop is not implemented yet (needs File/FROM_FILE edges)")
		case "--json":
			opt.JSONOut = true
		case "--no-web":
			opt.NoWeb = true
		case "--list-model":
			opt.ListModel = true
		default:
			if strings.HasPrefix(arg, "-") {
				return opt, fmt.Errorf("unknown flag %q", arg)
			}
			queryArgs = append(queryArgs, arg)
		}
	}

	opt.Query = strings.TrimSpace(strings.Join(queryArgs, " "))
	if opt.Query == "" && !opt.ListModel {
		return opt, fmt.Errorf("no query given")
	}
	return opt, nil
}
