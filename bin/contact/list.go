//usr/bin/env go run "$0" "$@"; exit
//go:build ignore
// bin/contact/list.go - read and normalize address-book files (csv/vcf/mab)
// to stdout or a file. Pure converter: no brain, no CRM.
//
//	go run ./bin/contact/import.go --sources a.csv --format json
//	go run ./bin/contact/import.go --sources dir/ --format csv --out out.csv
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/eSlider/2dph/pkg/contact"
	cliparse "github.com/eSlider/2dph/pkg/cli"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		sources []string
		format  = "json"
		out     string
		dryRun  bool
	)
	p := cliparse.New("contact-import")
	p.Description = "read address-book contacts (csv/vcf/mab) and emit json|csv|leaf"
	p.StringSlice(&sources, "s", "sources", "comma-separated files or dirs to read")
	p.String(&format, "", "format", "output format: json|csv|leaf")
	p.String(&out, "", "out", "write output to FILE instead of stdout")
	p.Bool(&dryRun, "", "dry-run", "print parsed counts only, write nothing")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "contact-import: --sources is required")
		return 2
	}
	var srcs []string
	for _, s := range sources {
		for _, part := range strings.Split(s, ",") {
			if p := strings.TrimSpace(part); p != "" {
				srcs = append(srcs, p)
			}
		}
	}
	cs, err := contact.Load(srcs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contact-import: %v\n", err)
		return 1
	}
	cs = contact.Dedupe(cs)
	if dryRun {
		contact.PrintCounts(cs)
		return 0
	}
	data, err := contact.Render(cs, format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contact-import: %v\n", err)
		return 1
	}
	if out != "" {
		if err := os.WriteFile(out, []byte(data), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "contact-import: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "contact-import: wrote %d to %s\n", len(cs), out)
		return 0
	}
	fmt.Print(data)
	return 0
}
