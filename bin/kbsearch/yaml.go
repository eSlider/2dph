// YAML emitter ported from bin/kb/yamlout.py — preserves insertion order.
package main

import (
	"fmt"
	"strconv"
	"strings"
)

// KV is an ordered key-value pair for maps.
type KV struct {
	K string
	V any
}

// Dict is an ordered map (slice of KV).
type Dict []KV

func toYAML(node any, indent int) string {
	pad := strings.Repeat("  ", indent)
	switch n := node.(type) {
	case Dict:
		if len(n) == 0 {
			return pad + "{}\n"
		}
		var b strings.Builder
		for _, kv := range n {
			switch nv := kv.V.(type) {
			case Dict:
				if len(nv) == 0 {
					b.WriteString(pad + kv.K + ": {}\n")
				} else {
					b.WriteString(pad + kv.K + ":\n" + toYAML(nv, indent+1))
				}
			case []any:
				if len(nv) == 0 {
					b.WriteString(pad + kv.K + ": []\n")
				} else {
					b.WriteString(pad + kv.K + ":\n" + toYAML(nv, indent+1))
				}
			default:
				b.WriteString(pad + kv.K + ": " + scalar(nv) + "\n")
			}
		}
		return b.String()

	case []any:
		if len(n) == 0 {
			return pad + "[]\n"
		}
		var b strings.Builder
		for _, item := range n {
			if d, ok := item.(Dict); ok {
				b.WriteString(pad + "-\n" + toYAML(d, indent+1))
			} else {
				b.WriteString(pad + "- " + scalar(item) + "\n")
			}
		}
		return b.String()

	default:
		return pad + scalar(node) + "\n"
	}
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return fmtFloat(t)
	case float32:
		return fmtFloat(float64(t))
	case string:
		return quoteIfNeeded(t)
	default:
		// fallback
		return fmt.Sprintf("%v", v)
	}
}

func fmtFloat(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

func quoteIfNeeded(s string) string {
	if strings.Contains(s, "\n") {
		return strconv.Quote(s)
	}
	if s == "" || strings.ContainsAny(s, ":#'\"[]{}&*!|>%@`") || s != strings.TrimSpace(s) {
		return strconv.Quote(s)
	}
	return s
}