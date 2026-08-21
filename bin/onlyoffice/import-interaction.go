//go:build onlyoffice_import_interaction

// usr/bin/env go run -tags=onlyoffice_import_interaction "$0" "$@"; exit
//
// bin/onlyoffice/import-interaction.go - restore email interactions into the
// OO CRM: for every raw mail whose sender matches a CRM person that takes part
// in at least one opportunity, attach a deterministic history note.
//
//	ONLYOFFICE_URL/USER/PASS ./bin/onlyoffice/import-interaction.go                # report only
//	./bin/onlyoffice/import-interaction.go --sources var/mail --write --limit 50
//
// OO history attaches to opportunities/cases only (no person history in this
// API version) — messages whose person has no opportunity are counted as
// no-opp and left to the brain, where mail content is already indexed.
//
// Idempotency: processed message keys live in var/state/mail-interactions.json
// (gitignored runtime state).
//
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/repo"
	"github.com/eslider/go-onlyoffice"
)

type state struct {
	Done map[string]bool `json:"done"`
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		sources string
		write   bool
		limit   int
	)
	p := cli.New("onlyoffice-import-interaction")
	p.Description = "restore email interactions as history notes on the person's opportunity"
	p.String(&sources, "", "sources", "var/mail root to scan (default var/mail)")
	p.Bool(&write, "", "write", "attach history notes (default: report only)")
	p.Int(&limit, "", "limit", "max notes to write this run (0 = all)")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	if sources == "" {
		sources = "var/mail"
	}
	msgs, skipped, err := mailconv.LoadMessages(sources)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import-interaction: %v\n", err)
		return 1
	}

	c := onlyoffice.NewClient(onlyoffice.GetEnvironmentCredentials())
	ctx := context.Background()

	// 1. email → person id.
	all, err := c.ListAllContacts(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import-interaction: list contacts: %v\n", err)
		return 1
	}
	personByEmail := map[string]string{}
	for _, person := range all {
		if isTrue(person["isCompany"]) {
			continue
		}
		id := fmt.Sprint(person["id"])
		for _, row := range onlyoffice.ContactInfoRows(person) {
			if onlyoffice.NormalizeContactInfoType(fmt.Sprint(row["infoType"])) != "email" {
				continue
			}
			data := strings.ToLower(strings.TrimSpace(fmt.Sprint(row["data"])))
			if data != "" && data != "<nil>" {
				personByEmail[data] = id
			}
		}
	}

	// 2. person id → first opportunity id (deterministic: lowest numeric).
	opps, err := c.ListAllOpportunities(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import-interaction: list opportunities: %v\n", err)
		return 1
	}
	oppByPerson := map[string]string{}
	for _, opp := range opps {
		oppID := fmt.Sprint(opp["id"])
		for _, m := range onlyoffice.OpportunityMembers(opp) {
			pid := fmt.Sprint(m["id"])
			if cur, ok := oppByPerson[pid]; !ok || oppLess(oppID, cur) {
				oppByPerson[pid] = oppID
			}
		}
	}

	stPath := filepath.Join(repo.Root(), "var", "state", "mail-interactions.json")
	st := state{Done: map[string]bool{}}
	if data, err := os.ReadFile(stPath); err == nil {
		_ = json.Unmarshal(data, &st)
	}

	sort.Slice(msgs, func(i, j int) bool { return msgs[i].ReceivedAt.Before(msgs[j].ReceivedAt) })
	var (
		written, noPerson, noOpp, machine, done int
		pending                                 int
	)
	for _, m := range msgs {
		key := m.Source + "/" + m.ID
		if st.Done[key] {
			done++
			continue
		}
		from := mailconv.ParseAddress(m.From)
		if from.Email == "" || mailconv.IsMachineSender(from) {
			machine++
			continue
		}
		personID, ok := personByEmail[from.Email]
		if !ok {
			noPerson++
			continue
		}
		oppID, ok := oppByPerson[personID]
		if !ok {
			noOpp++
			continue
		}
		pending++
		if !write || (limit > 0 && written >= limit) {
			continue
		}
		if _, err := c.AddHistoryNote(ctx, "opportunity", atoi(oppID), mailconv.FormatNote(m), 0); err != nil {
			fmt.Fprintf(os.Stderr, "  history msg %s: %v\n", key, err)
			continue
		}
		st.Done[key] = true
		written++
	}
	if write && written > 0 {
		_ = os.MkdirAll(filepath.Dir(stPath), 0o755)
		data, _ := json.MarshalIndent(st, "", "  ")
		_ = os.WriteFile(stPath, data, 0o644)
	}
	fmt.Fprintf(os.Stderr, "import-interaction: %d messages (%d unreadable) · done=%d machine=%d no-person=%d no-opp=%d pending=%d written=%d\n",
		len(msgs), skipped, done, machine, noPerson, noOpp, pending, written)
	return 0
}

func isTrue(v any) bool {
	b, _ := v.(bool)
	return b
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func oppLess(a, b string) bool {
	na, nb := atoi(a), atoi(b)
	if na != nb {
		return na < nb
	}
	return a < b
}
