//go:build contacts_import

// usr/bin/env go run -tags=contacts_import "$0" "$@"; exit
//
// bin/contacts/import.go - address books (VCF/MAB/CSV) → brain markdown +
// CRM reconcile by E.164/email (#85).
//
//	./bin/contacts/import.go                                  # md + report
//	./bin/contacts/import.go --root "/mnt/8TB/contacts" --write --limit 5
//
// Phones are normalized via internal/contact.NormalizePhone (nyaruka/
// phonenumbers): "+CC…", "00CC…", "0<trunk>", and bare "CC<national>" resolved
// by longest-prefix country-code match; ambiguous inputs keep every candidate.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/contact"
	"github.com/eSlider/2dph/pkg/repo"
	"github.com/eslider/go-onlyoffice"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		root    string
		outDir  string
		region  string
		exclude string
		write   bool
		limit   int
	)
	p := cli.New("contacts-import")
	p.Description = "address books → brain markdown + CRM reconcile by E.164/email"
	p.String(&root, "", "root", "address-book root to scan")
	p.String(&outDir, "", "out", "markdown output dir (default var/contacts-md)")
	p.String(&region, "", "region", "default region for phone parsing (DE)")
	p.String(&exclude, "", "exclude", "comma-separated path substrings to skip (Aleksey Krylov)")
	p.Bool(&write, "", "write", "create missing CRM persons (default report only)")
	p.Int(&limit, "", "limit", "max persons to create this run (0 = all)")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	if root == "" {
		root = "/mnt/8TB/contacts"
	}
	if outDir == "" {
		outDir = filepath.Join(repo.Root(), "var", "contacts-md")
	}
	if region == "" {
		region = "DE"
	}
	var excluded []string
	for _, e := range strings.Split(exclude, ",") {
		if e = strings.TrimSpace(e); e != "" {
			excluded = append(excluded, e)
		}
	}

	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		low := filepath.Ext(path)
		switch strings.ToLower(low) {
		case ".vcf", ".vcard", ".mab", ".csv":
		default:
			return nil
		}
		slashed := filepath.ToSlash(path)
		for _, ex := range excluded {
			if strings.Contains(slashed, ex) {
				return nil
			}
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "contacts-import: walk: %v\n", err)
		return 1
	}
	sort.Strings(files)

	var all []contact.Contact
	byFile := map[string][]contact.Contact{}
	for _, f := range files {
		cs, err := contact.Load([]string{f})
		if err != nil {
			fmt.Fprintf(os.Stderr, "contacts-import: %s: %v\n", f, err)
			continue
		}
		for i := range cs {
			cs[i].Source = f
			all = append(all, cs[i])
		}
		byFile[f] = cs
	}

	_ = os.MkdirAll(outDir, 0o755)
	var (
		withEmail, withPhone, badPhones int
		phoneNorms                      = map[string]contact.PhoneNorm{}
	)
	for _, c := range all {
		for _, ph := range c.Phones {
			n := contact.NormalizePhone(ph, region)
			phoneNorms[ph] = n
			if !n.Valid {
				badPhones++
				continue
			}
			if n.E164 != "" && !n.Ambiguous {
				withPhone++
			}
		}
		if len(c.Emails) > 0 {
			withEmail++
		}
	}
	for f, cs := range byFile {
		md := renderMD(f, cs, phoneNorms)
		flat := strings.NewReplacer("/", "__", "\\", "__").Replace(strings.TrimPrefix(filepath.ToSlash(f), filepath.ToSlash(root)+"/"))
		dst := filepath.Join(outDir, flat+".md")
		if err := os.WriteFile(dst, []byte(md), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "contacts-import: write %s: %v\n", dst, err)
			return 1
		}
	}

	crmMatched, crmNew, created := reconcileCRM(all, region, write, limit)

	fmt.Fprintf(os.Stderr, "contacts-import: %d files → %d contacts (%d md) · emails=%d phones-ok=%d phones-bad=%d · crm matched=%d new=%d created=%d\n",
		len(files), len(all), len(byFile), withEmail, withPhone, badPhones, crmMatched, crmNew, created)
	return 0
}

func renderMD(file string, cs []contact.Contact, norms map[string]contact.PhoneNorm) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Contacts: %s\n\n", filepath.Base(file))
	fmt.Fprintf(&b, "- Source: `%s`\n- Entries: %d\n\n", file, len(cs))
	for _, c := range cs {
		name := c.DisplayName()
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Fprintf(&b, "## %s\n", name)
		if c.Org != "" || c.Title != "" {
			fmt.Fprintf(&b, "- Role: %s %s\n", c.Org, c.Title)
		}
		for _, e := range c.Emails {
			fmt.Fprintf(&b, "- Email: %s\n", e)
		}
		for _, ph := range c.Phones {
			line := fmt.Sprintf("- Phone: %s", ph)
			if n, ok := norms[ph]; ok && n.Valid {
				if n.Ambiguous {
					line += fmt.Sprintf(" → ambiguous candidates %v", n.Candidates)
				} else {
					line += " → `" + n.E164 + "`"
				}
			} else if ok && !n.Valid {
				line += " → UNPARSEABLE"
			}
			b.WriteString(line + "\n")
		}
		fmt.Fprintln(&b)
	}
	return b.String()
}

func reconcileCRM(all []contact.Contact, region string, write bool, limit int) (matched, fresh, created int) {
	if os.Getenv("ONLYOFFICE_URL") == "" && os.Getenv("ONLYOFFICE_HOST") == "" {
		return -1, -1, 0 // no CRM env: report-only mode for local stats
	}
	c := onlyoffice.NewClient(onlyoffice.GetEnvironmentCredentials())
	ctx := context.Background()

	emailIdx, err := c.BuildContactEmailIndex(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contacts-import: email index: %v\n", err)
		return -1, -1, 0
	}
	allRows, err := c.ListAllContacts(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "contacts-import: contacts: %v\n", err)
		return -1, -1, 0
	}
	phoneIdx := map[string]string{}
	for _, person := range allRows {
		id := fmt.Sprint(person["id"])
		for _, row := range onlyoffice.ContactInfoRows(person) {
			t := onlyoffice.NormalizeContactInfoType(fmt.Sprint(row["infoType"]))
			if t != "phone" && t != "mobile" {
				continue
			}
			if n := contact.NormalizePhone(fmt.Sprint(row["data"]), region); n.Valid && !n.Ambiguous {
				phoneIdx[n.E164] = id
			}
		}
	}

	for _, con := range all {
		hit := false
		for _, e := range con.Emails {
			if _, ok := emailIdx[strings.ToLower(e)]; ok {
				hit = true
				break
			}
		}
		if !hit {
			for _, ph := range con.Phones {
				if n := contact.NormalizePhone(ph, region); n.Valid && !n.Ambiguous {
					if _, ok := phoneIdx[n.E164]; ok {
						hit = true
						break
					}
				}
			}
		}
		if hit {
			matched++
			continue
		}
		fresh++
		if !write || (limit > 0 && created >= limit) {
			continue
		}
		email := ""
		if len(con.Emails) > 0 {
			email = con.Emails[0]
		}
		given, family := splitName(con.DisplayName(), email)
		if _, err := c.CreatePerson(ctx, given, family, 0, con.Title, "imported from "+con.Source); err != nil {
			fmt.Fprintf(os.Stderr, "  create %q: %v\n", con.DisplayName(), err)
			continue
		}
		created++
	}
	return matched, fresh, created
}

func splitName(name, email string) (given, family string) {
	parts := strings.Fields(name)
	switch len(parts) {
	case 0:
		return nameFromEmail(email)
	case 1:
		return parts[0], ""
	default:
		return parts[0], parts[len(parts)-1]
	}
}

func nameFromEmail(email string) (string, string) {
	local := email
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	tok := strings.Split(local, ".")
	if len(tok) == 2 && tok[0] != "" && tok[1] != "" {
		return tok[0], tok[1]
	}
	return local, ""
}
