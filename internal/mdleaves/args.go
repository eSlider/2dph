package mdleaves

import (
	"github.com/eSlider/2dph/internal/cli"
	"github.com/integrii/flaggy"
)

type CLI struct {
	Root    string
	Files   string
	JSONOut bool
}

func Parser() *flaggy.Parser {
	c := CLI{Root: "."}
	return Bind(&c)
}

func Bind(c *CLI) *flaggy.Parser {
	if c.Root == "" {
		c.Root = "."
	}
	p := cli.New("markdown-import")
	p.Description = "split markdown H2 leafs"
	p.Bool(&c.JSONOut, "", "json", "JSON output")
	p.String(&c.Files, "", "files", "comma-separated paths")
	p.AddPositionalValue(&c.Root, "dir", 1, false, "markdown root")
	return p
}

func ParseArgs(args []string) (CLI, error) {
	c := CLI{Root: "."}
	p := Bind(&c)
	if err := cli.Parse(p, args); err != nil {
		return c, err
	}
	if extra := cli.Query("", p.TrailingArguments); extra != "" && c.Root == "." {
		c.Root = extra
	}
	return c, nil
}
