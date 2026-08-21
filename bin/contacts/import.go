package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	cliparse "github.com/eSlider/2dph/internal/cli"
)

// bin/contacts/import.go - ingest address-book files into the 2dph brain and
// the OnlyOffice CRM.
//
//	go run ./bin/contacts --sources /path/to/contacts.csv --dry-run
//	go run ./bin/contacts --sources /path/to/dir --format json --out out.json
//	go run ./bin/contacts --sources ... --write-brain   # needs cgo+system_ladybug
//	go run ./bin/contacts --sources ... --write-oo      # needs ONLYOFFICE_URL/USER/PASS
func main() {
	os.Exit(run(os.Args[1:]))
}

type flags struct {
	sources    []string
	format     string
	out        string
	dryRun     bool
	writeBrain bool
	writeOO    bool
	db         string
}

func run(args []string) int {
	v := flags{format: "json"}
	p := cliparse.New("contacts")
	p.Description = "import address-book contacts into the brain and OO CRM"
	p.StringSlice(&v.sources, "s", "sources", "comma-separated files or dirs to import")
	p.String(&v.format, "", "format", "output format: json|csv|leaf")
	p.String(&v.out, "", "out", "write output to FILE instead of stdout")
	p.Bool(&v.dryRun, "", "dry-run", "print parsed counts only, write nothing")
	p.Bool(&v.writeBrain, "", "write-brain", "upsert contacts into the 2dph brain (needs cgo+system_ladybug)")
	p.Bool(&v.writeOO, "", "write-oo", "reconcile contacts into the OnlyOffice CRM")
	p.String(&v.db, "", "db", "path to kb.lbug for --write-brain")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	if len(v.sources) == 0 {
		fmt.Fprintln(os.Stderr, "contacts: --sources is required (comma-separated files or dirs)")
		return 2
	}
	if v.writeBrain && v.writeOO {
		fmt.Fprintln(os.Stderr, "contacts: --write-brain and --write-oo are mutually exclusive")
		return 2
	}

	var srcs []string
	for _, s := range v.sources {
		for _, part := range strings.Split(s, ",") {
			if p := strings.TrimSpace(part); p != "" {
				srcs = append(srcs, p)
			}
		}
	}
	contacts, err := loadSources(srcs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contacts: %v\n", err)
		return 1
	}

	// Deduplicate by (display name, first email).
	contacts = dedupe(contacts)

	if v.dryRun {
		printCounts(contacts)
		return 0
	}
	if v.writeBrain {
		if err := brainWriteFunc(contacts, v.db); err != nil {
			fmt.Fprintf(os.Stderr, "contacts: %v\n", err)
			return 1
		}
		return 0
	}
	if v.writeOO {
		ctx := context.Background()
		created, matched, failed, err := writeOOContacts(ctx, contacts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "contacts: %v\n", err)
			return 1
		}
		printCounts(contacts)
		fmt.Fprintf(os.Stderr, "oo: created=%d matched=%d failed=%d\n", created, matched, failed)
		return 0
	}

	out, err := render(contacts, v.format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contacts: %v\n", err)
		return 1
	}
	if v.out != "" {
		if err := os.WriteFile(v.out, []byte(out), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "contacts: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "contacts: wrote %d to %s\n", len(contacts), v.out)
		return 0
	}
	fmt.Print(out)
	return 0
}

func printCounts(cs []Contact) {
	emails := 0
	phones := 0
	bySource := map[string]int{}
	for _, c := range cs {
		emails += len(c.Emails)
		phones += len(c.Phones)
		bySource[c.Source]++
	}
	fmt.Printf("contacts: %d total (%d emails, %d phones)\n", len(cs), emails, phones)
	for src, n := range bySource {
		fmt.Printf("  %4d  %s\n", n, src)
	}
}

func dedupe(cs []Contact) []Contact {
	seen := map[string]bool{}
	var out []Contact
	for _, c := range cs {
		key := strings.ToLower(strings.TrimSpace(c.DisplayName()))
		if len(c.Emails) > 0 {
			key += "|" + strings.ToLower(c.Emails[0])
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

func render(cs []Contact, format string) (string, error) {
	switch format {
	case "json":
		b, err := json.MarshalIndent(cs, "", "  ")
		return string(b), err
	case "csv":
		var b strings.Builder
		w := csv.NewWriter(&b)
		_ = w.Write([]string{"full_name", "given", "family", "emails", "phones", "org", "title", "photo", "source"})
		for _, c := range cs {
			_ = w.Write([]string{
				c.FullName, c.Given, c.Family,
				strings.Join(c.Emails, ";"), strings.Join(c.Phones, ";"),
				c.Org, c.Title, c.Photo, c.Source,
			})
		}
		w.Flush()
		return b.String(), w.Error()
	case "leaf":
		var b strings.Builder
		for _, c := range cs {
			b.WriteString(c.Markdown())
			b.WriteString("\n\n")
		}
		return b.String(), nil
	default:
		return "", fmt.Errorf("unknown format %q (want json|csv|leaf)", format)
	}
}
