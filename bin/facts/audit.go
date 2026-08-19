//usr/bin/env go run -tags=facts_audit "$0" "$@"; exit
//go:build facts_audit
//
// bin/facts/audit.go - 2-source + lexicon checks.
//
//	./bin/facts/audit.go self         # Go implementation (no python)
//	./bin/facts/audit.go db           # python bin/facts/audit db
//	./bin/facts/audit.go contradict   # python bin/facts/audit contradict
//
// `self` mode checks the repo itself (PLAN.md + README.md lexicons) and is
// implemented in Go so CI needs no python. `db`/`contradict` still delegate
// to the python implementation via cmdbin.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "self" {
		os.Exit(auditSelf())
	}
	os.Exit(cmdbin.ExecFile("bin/facts/audit", os.Args[1:]))
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

var (
	reTwoSource = regexp.MustCompile(`(?i)facts must have.*2 sources|2.source`)
	reSearch    = regexp.MustCompile(`(?i)HNSW|BM25|deduction`)
)