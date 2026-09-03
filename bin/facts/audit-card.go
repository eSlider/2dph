//usr/bin/env go run -tags=facts_audit_card "$0" "$@"; exit
//go:build facts_audit_card
//
// bin/facts/audit-card.go — Vinogradov audit card writer (L-9.2 #231).
//
//	./bin/facts/audit-card.go \
//		--claim "demo2 подтверждён вторым источником" \
//		--premises FACT-9002,FACT-9001 \
//		--inference deduction \
//		--gaps OPEN-0001 \
//		--counter none \
//		--verdict weaken
//
// Validates the card (verdict accept|reject|weaken; premises/inference/counter
// required), assigns the next AUD-NNNN id and appends it to audits[] of
// var/audit/source-of-truth.yml — comments and the other SoT sections survive
// untouched. Pure Go, no network.
// Exit 0 = appended, 1 = card invalid / SoT problem, 2 = usage.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/facts"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/repo"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		claim     string
		premises  []string
		inference = string(facts.InferenceDeduction)
		gaps      []string
		counter   = "none"
		verdict   string
		sot       string
	)
	p := cli.New("audit-card")
	p.Description = "append a Vinogradov audit card to SoT audits[]"
	p.String(&claim, "", "claim", "audited claim (required)")
	p.StringSlice(&premises, "", "premises", "premise FACT-/OPEN- id (repeatable or comma-separated)")
	p.String(&inference, "", "inference", "deduction|induction|analogy|other (default deduction)")
	p.StringSlice(&gaps, "", "gaps", "OPEN- gap id (repeatable or comma-separated, optional)")
	p.String(&counter, "", "counter", "counter-argument, use \"none\" when absent (default none)")
	p.String(&verdict, "", "verdict", "accept|reject|weaken (required)")
	p.String(&sot, "", "sot", "path to source-of-truth.yml (default <root>/var/audit/source-of-truth.yml)")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}

	v, i := facts.Verdict(verdict), facts.Inference(inference)
	if !v.Valid() {
		fmt.Fprintln(os.Stderr, "audit-card: --verdict must be accept|reject|weaken, got", verdict)
		return 2
	}
	if !i.Valid() {
		fmt.Fprintln(os.Stderr, "audit-card: --inference must be deduction|induction|analogy|other, got", inference)
		return 2
	}
	if sot == "" {
		sot = filepath.Join(repo.Root(), "var", "audit", "source-of-truth.yml")
	}

	c := facts.AuditCard{
		Date:      time.Now().Format("2006-01-02"),
		Claim:     claim,
		Premises:  splitList(premises),
		Inference: i,
		Gaps:      splitList(gaps),
		Counter:   counter,
		Verdict:   v,
	}
	saved, err := facts.AppendAuditCard(sot, c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "audit-card:", err)
		return 1
	}

	gapsLine := strings.Join(saved.Gaps, "; ")
	if gapsLine == "" {
		gapsLine = "-"
	}
	fmt.Printf("%s appended to %s\n", saved.ID, sot)
	fmt.Printf("Claim: %s\n", saved.Claim)
	fmt.Printf("Premises: %s\n", strings.Join(saved.Premises, "; "))
	fmt.Printf("Inference: %s\n", saved.Inference)
	fmt.Printf("Gaps: %s\n", gapsLine)
	fmt.Printf("Counter: %s\n", saved.Counter)
	fmt.Printf("Verdict: %s\n", saved.Verdict)
	return 0
}

// splitList splits each flag occurrence on commas and drops empties, so
// --premises A,B --premises C and --premises A --premises B,C behave the same.
func splitList(ss []string) []string {
	var out []string
	for _, s := range ss {
		for _, part := range strings.Split(s, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}
