//go:build onlyoffice_reconcile_contact

// usr/bin/env go run -tags=onlyoffice_reconcile_contact "$0" "$@"; exit
//
// bin/onlyoffice/reconcile-contact.go - read raw mail senders and reconcile
// them into the OnlyOffice CRM: match by email, create missing persons.
//
//	ONLYOFFICE_URL/USER/PASS ./bin/onlyoffice/reconcile-contact.go                 # report only
//	./bin/onlyoffice/reconcile-contact.go --sources var/mail --write --limit 200
//
// Machine senders (noreply/newsletter/bounces, mass platforms) never become
// contacts. Read-only by default; idempotent by email match.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eslider/go-onlyoffice"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		sources string
		write   bool
		limit   int
	)
	p := cli.New("onlyoffice-reconcile-contact")
	p.Description = "reconcile human mail senders into OO CRM persons (match by email)"
	p.String(&sources, "", "sources", "var/mail root to scan (default var/mail)")
	p.Bool(&write, "", "write", "create missing persons (default: report only)")
	p.Int(&limit, "", "limit", "max new persons to create this run (0 = all)")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	if sources == "" {
		sources = "var/mail"
	}
	msgs, skipped, err := mailconv.LoadMessages(sources)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reconcile-contact: %v\n", err)
		return 1
	}

	// Unique human senders, deterministic order.
	type cand struct {
		addr     mailconv.ParsedAddress
		messages int
	}
	seen := map[string]*cand{}
	var selfEmails = map[string]bool{"eslider@gmail.com": true}
	for _, m := range msgs {
		a := mailconv.ParseAddress(m.From)
		if a.Email == "" || selfEmails[a.Email] || mailconv.IsMachineSender(a) {
			continue
		}
		c, ok := seen[a.Email]
		if !ok {
			c = &cand{addr: a}
			seen[a.Email] = c
		}
		c.messages++
	}
	emails := make([]string, 0, len(seen))
	for e := range seen {
		emails = append(emails, e)
	}
	sort.Strings(emails)
	fmt.Fprintf(os.Stderr, "reconcile-contact: %d messages (%d unreadable), %d unique human senders\n",
		len(msgs), skipped, len(emails))

	c := onlyoffice.NewClient(onlyoffice.GetEnvironmentCredentials())
	ctx := context.Background()

	// One pass over all contacts → email→id index (per-sender FindPersonByEmail
	// would rescan the whole CRM for every candidate).
	idx, err := c.BuildContactEmailIndex(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reconcile-contact: contact index: %v\n", err)
		return 1
	}

	var matched, created, failed int
	for _, e := range emails {
		cd := seen[e]
		if _, hit := idx[e]; hit {
			matched++
			continue
		}
		if !write || (limit > 0 && created >= limit) {
			continue
		}
		given, family := mailconv.SplitPersonName(cd.addr.Name, cd.addr.Email)
		person, err := c.CreatePerson(ctx, given, family, 0, "", "")
		if err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "  create %s: %v\n", e, err)
			continue
		}
		id := fmt.Sprint(person["id"])
		if _, err := c.AddContactInfo(ctx, id, "email", e, "Work", true); err != nil {
			fmt.Fprintf(os.Stderr, "  info %s: %v\n", e, err)
		}
		created++
		fmt.Fprintf(os.Stderr, "  created %q <%s> -> %s (%d msgs)\n", cd.addr.Name, e, id, cd.messages)
	}
	fmt.Fprintf(os.Stderr, "reconcile-contact: matched=%d created=%d failed=%d pending=%d\n",
		matched, created, failed, len(emails)-matched-created-failed)
	if failed > 0 {
		return 1
	}
	return 0
}
