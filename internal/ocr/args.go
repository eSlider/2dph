package ocr

import (
	"fmt"

	"github.com/eSlider/2dph/internal/cli"
	"github.com/integrii/flaggy"
)

type CLI struct {
	Path string
}

func Parser() *flaggy.Parser {
	c := CLI{}
	return Bind(&c)
}

func Bind(c *CLI) *flaggy.Parser {
	p := cli.New("mail-ocr")
	p.Description = "tesseract eng+deu on image or scanned PDF"
	p.AddPositionalValue(&c.Path, "file", 1, false, "image or pdf")
	return p
}

func ParseArgs(args []string) (CLI, error) {
	var c CLI
	if err := cli.Parse(Bind(&c), args); err != nil {
		return c, err
	}
	if c.Path == "" {
		return c, fmt.Errorf("usage: bin/mail/ocr.go <image|pdf>")
	}
	return c, nil
}
