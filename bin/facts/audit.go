//usr/bin/env go run -tags=facts_audit "$0" "$@"; exit
//go:build facts_audit
//
// bin/facts/audit.go - 2-source + lexicon checks.
//
//	./bin/facts/audit.go self         # repo lexicons (Go, no deps)
//	./bin/facts/audit.go db           # evidence gate over var/kb.lbug (Go, via Zig)
//	./bin/facts/audit.go contradict   # D16 adjudication (Go; JSON claim(s) on stdin)
//
// `self` and `contradict` run pure Go. `db` builds the ladybug read via
// bin/facts/audit_db.go (bin/cgo/zig CGO toolchain).
// Exit 0 = all checks pass, 1 = audit failures, 2 = could not evaluate.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eSlider/2dph/internal/cmdbin"
	"github.com/eSlider/2dph/internal/facts"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: bin/facts/audit.go self|db|contradict [--json]")
		os.Exit(2)
	}
	switch args[0] {
	case "self":
		os.Exit(auditSelf())
	case "contradict":
		os.Exit(auditContradict())
	case "db":
		os.Exit(auditDB())
	default:
		fmt.Fprintln(os.Stderr, "audit: unknown mode:", args[0])
		os.Exit(2)
	}
}

func auditDB() int {
	root := cmdbin.Root()
	cmd := exec.Command(
		filepath.Join(root, "bin", "cgo", "zig"),
		"go", "run", "-tags=system_ladybug,facts_audit_db",
		filepath.Join(root, "bin", "facts", "audit_db.go"),
	)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = root
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "audit db:", err)
		return 2
	}
	return 0
}

func auditSelf() int {
	root := cmdbin.Root()
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit self: PLAN.md:", err)
		return 2
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit self: README.md:", err)
		return 2
	}

	var problems []string
	planS := string(plan)
	readmeS := string(readme)

	if !strings.Contains(planS, "recall@5") {
		problems = append(problems, "PLAN.md missing recall@5 gate")
	}
	if reTwoSource.MatchString(planS) == false {
		problems = append(problems, "PLAN.md missing the two-source evidence rule for facts")
	}
	if !strings.Contains(planS, "temporal_freshness") || !strings.Contains(planS, "authority_pairing") {
		problems = append(problems, "PLAN.md missing D16 adjudication rules")
	}
	if reSearch.MatchString(readmeS) == false {
		problems = append(problems, "README.md missing search/retrieval description")
	}

	if len(problems) == 0 {
		fmt.Println("audit self: ok")
		return 0
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "audit self:", p)
	}
	return 1
}

func auditContradict() int {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "contradict: read stdin:", err)
		return 2
	}
	if strings.TrimSpace(string(raw)) == "" {
		fmt.Fprintln(os.Stderr, "contradict: empty stdin (JSON claim or {claims:[...]})")
		return 1
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		fmt.Fprintln(os.Stderr, "contradict: invalid JSON:", err)
		return 1
	}
	claims, ok := collectClaims(payload)
	if !ok {
		fmt.Fprintln(os.Stderr, "contradict: expected object or list")
		return 1
	}
	results := make([]facts.Result, 0, len(claims))
	for _, c := range claims {
		results = append(results, facts.Adjudicate(c))
	}
	out := map[string]any{
		"mode":          "contradict",
		"ok":            true,
		"problems":      []string{},
		"contradictions": results,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "contradict: marshal:", err)
		return 2
	}
	fmt.Println(string(b))
	return 0
}

func collectClaims(payload any) ([]facts.Claim, bool) {
	switch v := payload.(type) {
	case []any:
		var out []facts.Claim
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, toClaim(m))
			}
		}
		return out, true
	case map[string]any:
		if claims, ok := v["claims"]; ok {
			if arr, ok := claims.([]any); ok {
				var out []facts.Claim
				for _, item := range arr {
					if m, ok := item.(map[string]any); ok {
						out = append(out, toClaim(m))
					}
				}
				return out, true
			}
		}
		return []facts.Claim{toClaim(v)}, true
	}
	return nil, false
}

func toClaim(m map[string]any) facts.Claim {
	c := facts.Claim{Text: fmt.Sprint(m["text"])}
	if yes, ok := m["yes"]; ok {
		c.Yes = toSources(yes)
	}
	if no, ok := m["no"]; ok {
		c.No = toSources(no)
	}
	return c
}

func toSources(v any) []facts.Source {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []facts.Source
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		s := facts.Source{
			ID:   strOf(m["id"]),
			Kind: strOf(m["kind"]),
			When: strOf(m["when"]),
		}
		if b, ok := m["stale"].(bool); ok {
			s.Stale = b
		}
		out = append(out, s)
	}
	return out
}

func strOf(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

var (
	reTwoSource = regexp.MustCompile(`(?i)facts must have.*2 sources|2.source`)
	reSearch    = regexp.MustCompile(`(?i)HNSW|BM25|deduction`)
)