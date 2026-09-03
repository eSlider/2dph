//usr/bin/env go run -tags=onlyoffice_import_network "$0" "$@"; exit
//go:build onlyoffice_import_network

// bin/onlyoffice/import-network.go - коннектор сети связей → OnlyOffice CRM
// (N-1.2 #269, epic #267): читает CRM-манифест accept-связей (выход
// bin/network --person X --accept-only, YAML network.Manifest, L-9.5 #234) и
// пишет каждую не-сервисную связь как контакт Person: имя (given/family из
// display name, fallback — локальная часть email), email (AddContactInfo
// primary), тег источника и about со сводкой msgs/threads/replies/period +
// premises-ссылками на Message/Commit графа. Идемпотентно по email:
// повторный прогон = 0 новых; существующие (включая импорт VCF/MAB #85)
// не перезаписываются.
//
// N-1.4 #271 (доп. шаги после создания, оба идемпотентны):
//   - --backfill: существующим matched-контактам дописывается тег источника
//     + about-premises-сводка аддитивно (ручные правки не перезаписываются);
//   - --companies <yaml>: компания-группировка role-ящиков по домену email
//     (kind=company): YAML "домен: Компания" (FindCompany/CreateCompany +
//     UpdatePerson; повтор = 0 изменений).
//
//	bin/network/network.go --person alice@example.com --accept-only > /tmp/net.yml
//	ONLYOFFICE_URL/USER/PASS ./bin/onlyoffice/import-network.go --manifest /tmp/net.yml            # report only
//	./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --dry-run                           # preview (lookups, ничего не пишет)
//	./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --limit 60                  # создать до 60
//	./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --backfill                  # + дописать тег/about matched
//	./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --companies companies.yml   # + role-ящики на компанию по домену
//	cat /tmp/net.yml | ./bin/onlyoffice/import-network.go --manifest - --write
//
// Маппинг (тело #269, epic #267): каждая accept-связь → контакт Person
// (name/email/тег/about). kind=company (роль-ящики info@/alle@, организации)
// тоже становится Person-контактом (компания-группировка по домену —
// N-1.4 #271); linkedin-релеи людей (hit-reply@linkedin.com с реальным
// display name) — Person как есть: email = релей, имя = реальный человек.
// kind=service (N-1.1 #268) пропускается. Секреты не логируются: creds —
// ONLYOFFICE_URL/USER/PASS (GetEnvironmentCredentials).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eSlider/2dph/internal/ooimport"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/utils"
	"github.com/eslider/go-onlyoffice"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		manifestPath  string
		dryRun        bool
		write         bool
		backfill      bool
		companiesPath string
		limit         int
		tag           string
	)
	p := cli.New("onlyoffice-import-network")
	p.Description = "import accept-links manifest (bin/network --accept-only) into OO CRM as Person contacts"
	p.String(&manifestPath, "", "manifest", "CRM manifest YAML from bin/network --accept-only ('-' = stdin)")
	p.Bool(&dryRun, "", "dry-run", "preview: план + lookups, ничего не пишет")
	p.Bool(&write, "", "write", "create missing persons (default: report only)")
	p.Bool(&backfill, "", "backfill", "add source tag + about summary to existing matched contacts (additive, N-1.4)")
	p.String(&companiesPath, "", "companies", "YAML domain→company mapping for role-box company grouping (N-1.4)")
	p.Int(&limit, "", "limit", "max new persons to create this run (0 = all)")
	p.String(&tag, "", "tag", "source contact tag (default 2dph:network:<target-email>)")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	if manifestPath == "" {
		fmt.Fprintln(os.Stderr, "onlyoffice-import-network: --manifest is required")
		return 2
	}
	if dryRun && write {
		fmt.Fprintln(os.Stderr, "onlyoffice-import-network: use --dry-run or --write, not both")
		return 2
	}
	if companiesPath == "-" && manifestPath == "-" {
		fmt.Fprintln(os.Stderr, "onlyoffice-import-network: --manifest and --companies cannot both read stdin")
		return 2
	}
	data, err := readManifest(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "onlyoffice-import-network: %v\n", err)
		return 1
	}
	m, err := ooimport.ParseManifest(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "onlyoffice-import-network: %v\n", err)
		return 1
	}
	plan, err := ooimport.BuildPlan(m, tag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "onlyoffice-import-network: %v\n", err)
		return 1
	}

	// N-1.4 #271: маппинг домен→компания парсится до OO-запросов (fail fast)
	var companyRules []ooimport.CompanyRule
	if companiesPath != "" {
		raw, err := readManifest(companiesPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "onlyoffice-import-network: companies: %v\n", err)
			return 1
		}
		companyRules, err = ooimport.ParseCompanies(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "onlyoffice-import-network: companies: %v\n", err)
			return 1
		}
		if len(companyRules) == 0 {
			fmt.Fprintln(os.Stderr, "onlyoffice-import-network: companies: пустой маппинг (нет правил домен→компания)")
			return 1
		}
	}

	reportOnly := !write
	fmt.Fprintf(os.Stderr, "onlyoffice-import-network: target=%s tag=%s links=%d skipped=%d\n",
		plan.Target, plan.Tag, len(m.Links), plan.Skipped)
	for _, ct := range plan.Contacts {
		name := strings.TrimSpace(ct.Given + " " + ct.Family)
		if name == "" {
			name = ct.Email
		}
		line := fmt.Sprintf("  %-42s %s", ct.Email, name)
		if reportOnly {
			line += "  | " + utils.Snippet(ct.About, 140)
		}
		fmt.Fprintln(os.Stderr, line)
	}

	c := onlyoffice.NewClient(onlyoffice.GetEnvironmentCredentials())
	ctx := context.Background()
	rep, err := ooimport.Run(ctx, c, plan, ooimport.Options{DryRun: reportOnly, Limit: limit})
	if err != nil {
		fmt.Fprintf(os.Stderr, "onlyoffice-import-network: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "oo: created=%d matched=%d skipped=%d failed=%d pending=%d\n",
		rep.Created, rep.Matched, rep.Skipped, rep.Failed, rep.Pending)
	if reportOnly {
		fmt.Fprintf(os.Stderr, "oo: report-only: %d новых было бы создано, ничего не записано\n", rep.New)
	}
	for _, f := range rep.Failures {
		fmt.Fprintf(os.Stderr, "oo: failed %s: %s\n", f.Email, f.Err)
	}
	if rep.Failed > 0 {
		return 1
	}

	// N-1.4 #271: дописывание тега/about существующим matched (аддитивно)
	if backfill {
		repBf, err := ooimport.RunBackfill(ctx, c, plan, ooimport.Options{DryRun: reportOnly})
		if err != nil {
			fmt.Fprintf(os.Stderr, "onlyoffice-import-network: backfill: %v\n", err)
			return 1
		}
		state := fmt.Sprintf("updated=%d unchanged=%d missing=%d failed=%d", repBf.Updated, repBf.Unchanged, repBf.Missing, repBf.Failed)
		if reportOnly {
			state = fmt.Sprintf("would=%d unchanged=%d missing=%d failed=%d", repBf.Would, repBf.Unchanged, repBf.Missing, repBf.Failed)
		}
		fmt.Fprintf(os.Stderr, "oo: backfill %s\n", state)
		for _, f := range repBf.Failures {
			fmt.Fprintf(os.Stderr, "oo: backfill failed %s: %s\n", f.Email, f.Err)
		}
		if repBf.Failed > 0 {
			return 1
		}
	}

	// N-1.4 #271: компания-группировка role-ящиков по домену (--companies YAML)
	if len(companyRules) > 0 {
		repCo, err := ooimport.RunCompanies(ctx, c, plan.Contacts, companyRules, ooimport.Options{DryRun: reportOnly})
		if err != nil {
			fmt.Fprintf(os.Stderr, "onlyoffice-import-network: companies: %v\n", err)
			return 1
		}
		state := fmt.Sprintf("found=%d created=%d linked=%d already=%d notfound=%d unmapped=%d failed=%d",
			repCo.CompaniesFound, repCo.CompaniesCreated, repCo.Linked, repCo.AlreadyLinked, repCo.NotFound, repCo.Unmapped, repCo.Failed)
		if reportOnly {
			state = fmt.Sprintf("found=%d missing=%d wouldlink=%d already=%d notfound=%d unmapped=%d failed=%d",
				repCo.CompaniesFound, repCo.CompaniesMissing, repCo.WouldLink, repCo.AlreadyLinked, repCo.NotFound, repCo.Unmapped, repCo.Failed)
		}
		fmt.Fprintf(os.Stderr, "oo: companies %s\n", state)
		for _, f := range repCo.Failures {
			fmt.Fprintf(os.Stderr, "oo: companies failed %s: %s\n", f.Email, f.Err)
		}
		if repCo.Failed > 0 {
			return 1
		}
	}
	return 0
}

func readManifest(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
