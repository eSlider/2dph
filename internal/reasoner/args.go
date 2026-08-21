package reasoner

import (
	"os"

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
	base := os.Getenv("REASONER_BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:11435/v1"
	}
	model := os.Getenv("REASONER_MODEL")
	if model == "" {
		model = OllamaRAM
	}
	return CLI{Base: base, Model: model, Device: "cpu"}
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
