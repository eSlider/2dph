package cli

import "github.com/integrii/flaggy"

type QAStats struct {
	JSONL string
}

func QAParser() *flaggy.Parser {
	c := QAStats{}
	return BindQA(&c)
}

func BindQA(c *QAStats) *flaggy.Parser {
	p := New("qa-stats")
	p.Description = "DuckDB quantiles / JSONL count"
	p.String(&c.JSONL, "", "jsonl", "JSONL file (else stdin JSON [float,…])")
	return p
}

func ParseQAStats(args []string) (QAStats, error) {
	var c QAStats
	return c, Parse(BindQA(&c), args)
}
