package gitlog

import (
	"fmt"
	"time"

	"github.com/eSlider/2dph/internal/cli"
	"github.com/integrii/flaggy"
)

type CLI struct {
	Repo, Root, Since string
	Limit             int
	JSONOut           bool
}

func Parser() *flaggy.Parser {
	c := CLI{}
	return Bind(&c)
}

func Bind(c *CLI) *flaggy.Parser {
	p := cli.New("git-import")
	p.Description = "go-git history → commit leafs"
	p.Bool(&c.JSONOut, "", "json", "JSON output")
	p.Int(&c.Limit, "", "limit", "max commits (0 = all)")
	p.String(&c.Since, "", "since", "RFC3339 or YYYY-MM-DD")
	p.String(&c.Root, "", "root", "scan dir for git repos")
	p.AddPositionalValue(&c.Repo, "repo", 1, false, "git repo path")
	return p
}

func ParseArgs(args []string) (CLI, error) {
	var c CLI
	if err := cli.Parse(Bind(&c), args); err != nil {
		return c, err
	}
	return c, nil
}

func ParseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse --since %q", s)
}
