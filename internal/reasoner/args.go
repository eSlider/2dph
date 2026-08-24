package reasoner

import (
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/integrii/flaggy"
)

type CLI struct {
	Base    string
	Model   string
	Device  string
	JSONOut bool
}

func Parser() *flaggy.Parser {
	c := NewCLI()
	return Bind(&c)
}

func NewCLI() CLI {
	c := LoadEnv()
	return CLI{Base: c.BaseURL, Model: c.Model, Device: c.Device}
}

func Bind(c *CLI) *flaggy.Parser {
	p := cli.New("reasoner-bench")
	p.Description = "CPU tool-call bake-off"
	p.Bool(&c.JSONOut, "", "json", "JSON output")
	p.String(&c.Model, "", "model", "Ollama/HF model id")
	p.String(&c.Base, "", "base-url", "OpenAI-compatible URL")
	p.String(&c.Device, "", "device", "cpu")
	return p
}

func ParseArgs(args []string) (CLI, error) {
	c := NewCLI()
	return c, cli.Parse(Bind(&c), args)
}
