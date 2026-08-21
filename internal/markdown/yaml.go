package markdown

import (
	"fmt"
	"strconv"
	"strings"
)

func EncodeYAML(leafs []Leaf) string {
	if len(leafs) == 0 {
		return "[]\n"
	}
	var b strings.Builder
	for _, lf := range leafs {
		b.WriteString("-\n")
		writeKV(&b, "source", lf.Source)
		writeKV(&b, "repo", lf.Repo)
		writeKV(&b, "heading", lf.Heading)
		writeKV(&b, "text", lf.Text)
		writeKV(&b, "type", lf.Type)
		writeKV(&b, "status", lf.Status)
		writeKV(&b, "related", lf.Related)
	}
	return b.String()
}

func writeKV(b *strings.Builder, k, v string) {
	fmt.Fprintf(b, "  %s: %s\n", k, yamlScalar(v))
}

func yamlScalar(s string) string {
	if strings.Contains(s, "\n") || s == "" || strings.ContainsAny(s, ":#'\"[]{}&*!|>%@`") || s != strings.TrimSpace(s) {
		return strconv.Quote(s)
	}
	return s
}
