package rank

import (
	"fmt"

	"github.com/eSlider/2dph/internal/cli"
	"github.com/integrii/flaggy"
)

type GetOptions struct {
	ID      string
	Body    bool
	JSONOut bool
}

func GetParser(opt *GetOptions) *flaggy.Parser {
	p := cli.New("brain-get")
	p.Description = "read one leaf"
	p.Bool(&opt.Body, "", "body", "full text instead of snippet")
	p.Bool(&opt.JSONOut, "", "json", "JSON output")
	p.AddPositionalValue(&opt.ID, "id", 1, false, "leaf id")
	return p
}

func ParseGet(args []string) (GetOptions, error) {
	var opt GetOptions
	if err := cli.Parse(GetParser(&opt), args); err != nil {
		return opt, err
	}
	if opt.ID == "" {
		return opt, fmt.Errorf("id required")
	}
	return opt, nil
}

type JSONFlag struct {
	JSONOut bool
}

func bindJSON(name string, opt *JSONFlag) *flaggy.Parser {
	p := cli.New(name)
	p.Bool(&opt.JSONOut, "", "json", "JSON output")
	return p
}

func StatsParser() *flaggy.Parser {
	opt := JSONFlag{}
	return bindJSON("brain-stats", &opt)
}

func EvalParser() *flaggy.Parser {
	opt := JSONFlag{}
	return bindJSON("brain-eval", &opt)
}

func ParseJSONFlag(name string, args []string) (JSONFlag, error) {
	var opt JSONFlag
	return opt, cli.Parse(bindJSON(name, &opt), args)
}
