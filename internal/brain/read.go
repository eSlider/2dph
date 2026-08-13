//go:build cgo && system_ladybug

package brain

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/eSlider/2dph/internal/brain/rank"
)

func MainGet(args []string) int {
	id, body, jsonOut := "", false, false
	for _, a := range args {
		switch {
		case a == "--body":
			body = true
		case a == "--json":
			jsonOut = true
		case a == "-h" || a == "--help":
			fmt.Fprintln(os.Stderr, `usage: bin/brain/get.go <id> [--body] [--json]`)
			return 0
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "brain/get: unknown flag %s\n", a)
			return 2
		default:
			id = a
		}
	}
	if id == "" {
		fmt.Fprintln(os.Stderr, "brain/get: id required")
		return 2
	}
	if err := openBrain(); err != nil {
		fmt.Fprintf(os.Stderr, "open brain: %v\n", err)
		return 1
	}
	defer closeBrain()
	meta, text, err := lookupLeaf(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/get: %v\n", err)
		return 1
	}
	out := Dict{
		{"id", meta["id"]},
		{"root", meta["root"]},
		{"confidence", meta["confidence"]},
		{"source", meta["source"]},
		{"type", meta["type"]},
	}
	if body {
		out = append(out, KV{"text", text})
	} else {
		out = append(out, KV{"snippet", clip(text, 280)})
	}
	if jsonOut {
		m := map[string]any{}
		for _, kv := range out {
			m[kv.K] = kv.V
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return b2i(enc.Encode(m))
	}
	fmt.Print(toYAML(out, 0))
	return 0
}

func MainStats(args []string) int {
	jsonOut := false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, `usage: bin/brain/stats.go [--json]`)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "brain/stats: unknown flag %s\n", a)
				return 2
			}
		}
	}
	if err := openBrain(); err != nil {
		fmt.Fprintf(os.Stderr, "open brain: %v\n", err)
		return 1
	}
	defer closeBrain()
	s, err := leafStats()
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/stats: %v\n", err)
		return 1
	}
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return b2i(enc.Encode(s))
	}
	by := s["by_root"].(map[string]int)
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	byRoot := make(Dict, 0, len(keys))
	for _, k := range keys {
		byRoot = append(byRoot, KV{k, by[k]})
	}
	out := Dict{
		{"total", s["total"]},
		{"by_root", byRoot},
		{"db", s["db"]},
		{"model", s["model"]},
	}
	fmt.Print(toYAML(out, 0))
	return 0
}

func MainEval(args []string) int {
	jsonOut := false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		case "-h", "--help":
			fmt.Fprintln(os.Stderr, `usage: bin/brain/eval.go [--json]`)
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "brain/eval: unknown flag %s\n", a)
				return 2
			}
		}
	}
	if err := openBrain(); err != nil {
		fmt.Fprintf(os.Stderr, "open brain: %v\n", err)
		return 1
	}
	defer closeBrain()
	recalled := 0
	details := make([]any, 0, len(rank.EvalQuestions))
	jsDetails := make([]map[string]any, 0, len(rank.EvalQuestions))
	for _, q := range rank.EvalQuestions {
		hits, err := queryFTS(q.Query, 5)
		ok := false
		if err == nil {
			frag := strings.ToLower(q.Fragment)
			for _, h := range hits {
				if strings.Contains(strings.ToLower(h.Text), frag) {
					ok = true
					break
				}
			}
		}
		if ok {
			recalled++
		}
		details = append(details, Dict{
			{"q", q.Query},
			{"fragment", q.Fragment},
			{"in_top5", ok},
		})
		jsDetails = append(jsDetails, map[string]any{
			"q": q.Query, "fragment": q.Fragment, "in_top5": ok,
		})
	}
	n := len(rank.EvalQuestions)
	recall := 0.0
	if n > 0 {
		recall = float64(recalled) / float64(n)
	}
	passed := recall >= rank.EvalRecallThreshold
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(map[string]any{
			"recall@5": round3(recall),
			"passed":   passed,
			"gate":     n,
			"details":  jsDetails,
		})
	} else {
		out := Dict{
			{"recall@5", round3(recall)},
			{"passed", passed},
			{"gate", n},
			{"details", details},
		}
		fmt.Print(toYAML(out, 0))
	}
	if !passed {
		return 2
	}
	return 0
}

func lookupLeaf(id string) (map[string]string, string, error) {
	if conn == nil {
		return nil, "", fmt.Errorf("brain not open")
	}
	stmt, err := conn.Prepare(
		"MATCH (l:Leaf {id:$id}) RETURN l.id, l.text, l.root, l.confidence, l.source, l.type",
	)
	if err != nil {
		return nil, "", err
	}
	defer stmt.Close()
	res, err := conn.Execute(stmt, map[string]any{"id": id})
	if err != nil {
		return nil, "", err
	}
	if !res.HasNext() {
		return nil, "", fmt.Errorf("no leaf %s", id)
	}
	row, err := res.Next()
	if err != nil {
		return nil, "", err
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) < 6 {
		return nil, "", fmt.Errorf("leaf row")
	}
	meta := map[string]string{
		"id":         fmt.Sprint(vals[0]),
		"root":       fmt.Sprint(vals[2]),
		"confidence": fmt.Sprint(vals[3]),
		"source":     fmt.Sprint(vals[4]),
		"type":       fmt.Sprint(vals[5]),
	}
	return meta, fmt.Sprint(vals[1]), nil
}

func leafStats() (map[string]any, error) {
	if conn == nil {
		return nil, fmt.Errorf("brain not open")
	}
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, count(*)")
	if err != nil {
		return nil, err
	}
	byRoot := map[string]int{}
	total := 0
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			continue
		}
		n := int(asInt(vals[1]))
		byRoot[fmt.Sprint(vals[0])] = n
		total += n
	}
	return map[string]any{
		"total":   total,
		"by_root": byRoot,
		"db":      dbPath(),
		"model":   ModelID,
	}, nil
}

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func round3(f float64) float64 {
	return float64(int(f*1000+0.5)) / 1000
}
