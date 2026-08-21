//usr/bin/env go run "$0" "$@"; exit
//
// bin/onlyoffice/import-contact.go - read address-book files and reconcile
// each contact into the OnlyOffice CRM (best effort; skip existing by email).
//
//	ONLYOFFICE_URL/USER/PASS ./bin/onlyoffice/import-contact.go --sources a.csv
//	./bin/onlyoffice/import-contact.go --sources dir/ --dry-run
//
// Uses the go-onlyoffice library client (FindPersonByEmail is idempotent:
// matches commonData emails, so re-runs never duplicate).
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/contact"
	"github.com/eslider/go-onlyoffice"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		sources []string
		dryRun  bool
	)
	p := cli.New("onlyoffice-import-contact")
	p.Description = "reconcile address-book contacts into the OnlyOffice CRM"
	p.StringSlice(&sources, "s", "sources", "comma-separated files or dirs to read")
	p.Bool(&dryRun, "", "dry-run", "print parsed counts only, write nothing")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "onlyoffice-import-contact: --sources is required")
		return 2
	}
	var srcs []string
	for _, s := range sources {
		for _, part := range strings.Split(s, ",") {
			if part = strings.TrimSpace(part); part != "" {
				srcs = append(srcs, part)
			}
		}
	}
	cs, err := contact.Load(srcs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "onlyoffice-import-contact: %v\n", err)
		return 1
	}
	cs = contact.Dedupe(cs)
	contact.PrintCounts(cs)
	if dryRun {
		return 0
	}

	c := onlyoffice.NewClient(onlyoffice.GetEnvironmentCredentials())
	ctx := context.Background()
	var created, matched, failed int
	for _, ct := range cs {
		email := first(ct.Emails)
		existing, err := c.FindPersonByEmail(ctx, email)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "oo lookup %q: %v\n", ct.DisplayName(), err)
			continue
		}
		if existing != nil {
			matched++
			fmt.Fprintf(os.Stderr, "oo matched %q (%s)\n", ct.DisplayName(), email)
			continue
		}
		person, err := c.CreatePerson(ctx, ct.Given, ct.Family, 0, ct.Title, ct.Org)
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "oo create %q: %v\n", ct.DisplayName(), err)
			continue
		}
		id := fmt.Sprint(person["id"])
		for i, e := range ct.Emails {
			_, _ = c.AddContactInfo(ctx, id, "email", e, "Work", i == 0)
		}
		for _, ph := range ct.Phones {
			_, _ = c.AddContactInfo(ctx, id, "phone", ph, "Work", false)
		}
		created++
		fmt.Fprintf(os.Stderr, "oo created %q -> %s\n", ct.DisplayName(), id)
	}
	fmt.Fprintf(os.Stderr, "oo: created=%d matched=%d failed=%d\n", created, matched, failed)
	return 0
}

func first(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}
