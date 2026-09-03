//go:build integration

package ooimport

// Интеграция коннектора с живым OnlyOffice (N-1.2 #269), по образцу
// go-onlyoffice (*_integration_test.go): creds ONLYOFFICE_URL/USER/PASS из
// окружения; без creds — skip (go test ./... остаётся зелёным офлайн).
//
// Прогон: dry-run (ничего не пишет) → write (контакт создан с email/тегом/
// about) → повтор (идемпотентность: 0 новых, все matched). Созданный
// тестовый контакт удаляется в конце (t.Cleanup); тег остаётся 0-count —
// API клиента не предоставляет удаление тега (отмечено в #269).

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/network"
	"github.com/eslider/go-onlyoffice"
)

func TestIntegrationImportNetwork(t *testing.T) {
	creds := onlyoffice.GetEnvironmentCredentials()
	if creds.Url == "" || creds.User == "" || creds.Password == "" {
		t.Skip("ONLYOFFICE_URL/USER/PASS not set")
	}
	c := onlyoffice.NewClient(creds)
	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	email := "import-network-test-" + suffix + "@example.com"
	tag := "2dph:network:it-" + suffix

	t.Cleanup(func() {
		if p, err := c.FindPersonByEmail(ctx, email); err == nil && p != nil {
			_, _ = c.DeleteContact(ctx, onlyoffice.ContactID(p))
		}
	})

	manifest := &network.Manifest{
		Target: "alice@example.com",
		Source: "2dph graph mail+git (L-9.5 #234)",
		Links: []network.ManifestLink{
			{
				Person: email, Name: "Network Import Test", Kind: "person",
				Msgs: 2, Threads: 1, Replies: 1, Period: "2026-08-01..2026-08-31",
				Premises: []network.Premise{{Kind: "mail", Ref: "it-" + suffix, Date: "2026-08-01"}},
			},
			{
				Person: "gitlab@example.com", Name: "GitLab", Kind: "service",
				Msgs: 5, Threads: 1, Replies: 0, Period: "2026-08-01..2026-08-31",
			},
		},
	}
	plan, err := BuildPlan(manifest, tag)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if plan.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (service)", plan.Skipped)
	}

	// dry-run: ничего не пишет
	rep, err := Run(ctx, c, plan, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run dry-run: %v", err)
	}
	if rep.Created != 0 || rep.Matched != 0 || rep.New != 1 {
		t.Errorf("dry-run report = %+v, want created=0 matched=0 new=1", rep)
	}
	if p, err := c.FindPersonByEmail(ctx, email); err != nil || p != nil {
		t.Fatalf("dry-run создал контакт? person=%v err=%v", p != nil, err)
	}

	// write: контакт создан с email/тегом/about
	rep, err = Run(ctx, c, plan, Options{})
	if err != nil {
		t.Fatalf("Run write: %v", err)
	}
	if rep.Created != 1 || rep.Matched != 0 || rep.Failed != 0 || rep.Pending != 0 {
		t.Errorf("write report = %+v, want created=1", rep)
	}
	person, err := c.FindPersonByEmail(ctx, email)
	if err != nil {
		t.Fatalf("FindPersonByEmail: %v", err)
	}
	if person == nil {
		t.Fatal("created person not found by email")
	}
	id := onlyoffice.ContactID(person)
	if got, err := c.GetContact(ctx, id); err != nil || got == nil {
		t.Fatalf("GetContact(%s): %v", id, err)
	} else if about := fmt.Sprint(got["about"]); !strings.Contains(about, "2 писем / 1 тредов / 1 ответов") ||
		!strings.Contains(about, "premises: mail it-") || !strings.Contains(about, "source: 2dph graph mail+git") {
		t.Errorf("about = %q, want сводку msgs/threads/replies + premises + source", about)
	}
	byTag, total, err := c.ListContactsByTag(ctx, tag, 50, 0)
	if err != nil {
		t.Fatalf("ListContactsByTag: %v", err)
	}
	found := false
	for _, p := range byTag {
		if onlyoffice.ContactID(p) == id {
			found = true
		}
	}
	if total == 0 || !found {
		t.Errorf("contact %s не найден по тегу %q (total=%d)", id, tag, total)
	}

	// повтор: 0 новых / все matched (идемпотентность по email)
	rep, err = Run(ctx, c, plan, Options{})
	if err != nil {
		t.Fatalf("Run repeat: %v", err)
	}
	if rep.Created != 0 || rep.Matched != 1 || rep.Failed != 0 {
		t.Errorf("repeat report = %+v, want created=0 matched=1", rep)
	}
}

// TestIntegrationImportNetworkLimit — --limit режет создание, остальное
// остаётся pending; повтор после добивания = 0 новых.
func TestIntegrationImportNetworkLimit(t *testing.T) {
	creds := onlyoffice.GetEnvironmentCredentials()
	if creds.Url == "" || creds.User == "" || creds.Password == "" {
		t.Skip("ONLYOFFICE_URL/USER/PASS not set")
	}
	c := onlyoffice.NewClient(creds)
	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	tag := "2dph:network:it-" + suffix
	var links []network.ManifestLink
	for _, who := range []string{"lima", "mike"} {
		e := "import-network-" + who + "-" + suffix + "@example.com"
		links = append(links, network.ManifestLink{
			Person: e, Name: "Limit " + who, Kind: "person",
			Msgs: 2, Threads: 1, Replies: 0, Period: "2026-08-01..2026-08-31",
		})
		t.Cleanup(func() {
			if p, err := c.FindPersonByEmail(ctx, e); err == nil && p != nil {
				_, _ = c.DeleteContact(ctx, onlyoffice.ContactID(p))
			}
		})
	}
	plan, err := BuildPlan(&network.Manifest{Target: "alice@example.com", Links: links}, tag)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(ctx, c, plan, Options{Limit: 1})
	if err != nil {
		t.Fatalf("Run limit=1: %v", err)
	}
	if rep.Created != 1 || rep.Pending != 1 || rep.Failed != 0 {
		t.Errorf("limit report = %+v, want created=1 pending=1", rep)
	}
	rep, err = Run(ctx, c, plan, Options{})
	if err != nil {
		t.Fatalf("Run rest: %v", err)
	}
	if rep.Created != 1 || rep.Matched != 1 || rep.Pending != 0 {
		t.Errorf("rest report = %+v, want created=1 matched=1", rep)
	}
}

// TestIntegrationCompaniesLinkRoleBoxes — компания-группировка (N-1.4 #271):
// role-ящик (kind=company) домена линкуется на компанию FindCompany/
// CreateCompany + UpdatePerson; повтор = 0 изменений (AlreadyLinked).
func TestIntegrationCompaniesLinkRoleBoxes(t *testing.T) {
	creds := onlyoffice.GetEnvironmentCredentials()
	if creds.Url == "" || creds.User == "" || creds.Password == "" {
		t.Skip("ONLYOFFICE_URL/USER/PASS not set")
	}
	c := onlyoffice.NewClient(creds)
	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	domain := "role-" + suffix + ".example"
	roleEmail := "info@" + domain
	companyName := "2dph N1.4 Test " + suffix
	tag := "2dph:network:it-" + suffix

	t.Cleanup(func() { // компания удаляется после person (LIFO)
		if co, err := c.FindCompany(ctx, companyName); err == nil && co != nil {
			_, _ = c.DeleteContact(ctx, onlyoffice.ContactID(co))
		}
	})
	t.Cleanup(func() {
		if p, err := c.FindPersonByEmail(ctx, roleEmail); err == nil && p != nil {
			_, _ = c.DeleteContact(ctx, onlyoffice.ContactID(p))
		}
	})

	plan, err := BuildPlan(&network.Manifest{
		Target: "alice@example.com",
		Source: "2dph graph mail+git (L-9.5 #234)",
		Links: []network.ManifestLink{
			{Person: roleEmail, Name: "WhereGroup Info", Kind: "company",
				Msgs: 5, Threads: 3, Replies: 0, Period: "2026-08-01..2026-08-31"},
		},
	}, tag)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(ctx, c, plan, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Created != 1 {
		t.Fatalf("Run report = %+v, want created=1", rep)
	}

	// dry-run: компания не создаётся, линковка только в отчёте
	repCo, err := RunCompanies(ctx, c, plan.Contacts, []CompanyRule{{Domain: domain, Name: companyName}}, Options{DryRun: true})
	if err != nil {
		t.Fatalf("RunCompanies dry-run: %v", err)
	}
	if repCo.CompaniesCreated != 0 || repCo.CompaniesMissing != 1 || repCo.Linked != 0 || repCo.WouldLink != 1 {
		t.Errorf("dry-run company report = %+v, want missing=1 wouldLink=1", repCo)
	}
	if co, err := c.FindCompany(ctx, companyName); err != nil || co != nil {
		t.Fatalf("dry-run создал компанию? company=%v err=%v", co != nil, err)
	}

	// write: компания создана, role-ящик слинкован
	repCo, err = RunCompanies(ctx, c, plan.Contacts, []CompanyRule{{Domain: domain, Name: companyName}}, Options{})
	if err != nil {
		t.Fatalf("RunCompanies: %v", err)
	}
	if repCo.CompaniesFound != 0 || repCo.CompaniesCreated != 1 || repCo.Linked != 1 ||
		repCo.AlreadyLinked != 0 || repCo.Unmapped != 0 || repCo.Failed != 0 {
		t.Errorf("company report = %+v, want created=1 linked=1", repCo)
	}
	company, err := c.FindCompany(ctx, companyName)
	if err != nil || company == nil {
		t.Fatalf("FindCompany(%q): company=%v err=%v", companyName, company != nil, err)
	}
	companyID := onlyoffice.ContactID(company)
	person, err := c.FindPersonByEmail(ctx, roleEmail)
	if err != nil || person == nil {
		t.Fatalf("FindPersonByEmail: person=%v err=%v", person != nil, err)
	}
	personID := onlyoffice.ContactID(person)
	full, err := c.GetContact(ctx, personID)
	if err != nil {
		t.Fatalf("GetContact: %v", err)
	}
	if got := curCompanyID(full); got == 0 || fmt.Sprint(got) != companyID {
		t.Errorf("person company id = %d, want %s", got, companyID)
	}
	members, err := c.ListCompanyPersons(ctx, companyID)
	if err != nil {
		t.Fatalf("ListCompanyPersons: %v", err)
	}
	found := false
	for _, m := range members {
		if onlyoffice.ContactID(m) == personID {
			found = true
		}
	}
	if !found {
		t.Errorf("person %s не в ListCompanyPersons(%s): %v", personID, companyID, members)
	}

	// повтор: компания найдена, role-ящик уже на ней → 0 изменений
	repCo, err = RunCompanies(ctx, c, plan.Contacts, []CompanyRule{{Domain: domain, Name: companyName}}, Options{})
	if err != nil {
		t.Fatalf("RunCompanies repeat: %v", err)
	}
	if repCo.CompaniesFound != 1 || repCo.CompaniesCreated != 0 || repCo.Linked != 0 || repCo.AlreadyLinked != 1 {
		t.Errorf("repeat company report = %+v, want found=1 already=1", repCo)
	}
}

// TestIntegrationBackfillExisting — дописывание тега+about существующим
// matched-контактам (N-1.4 #271): аддитивно (ручной about не перезаписан),
// повтор = 0 изменений.
func TestIntegrationBackfillExisting(t *testing.T) {
	creds := onlyoffice.GetEnvironmentCredentials()
	if creds.Url == "" || creds.User == "" || creds.Password == "" {
		t.Skip("ONLYOFFICE_URL/USER/PASS not set")
	}
	c := onlyoffice.NewClient(creds)
	ctx := context.Background()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	emailA := "backfill-a-" + suffix + "@example.com"
	emailB := "backfill-b-" + suffix + "@example.com"
	tag := "2dph:network:bf-" + suffix

	for _, e := range []string{emailA, emailB} {
		t.Cleanup(func() {
			if p, err := c.FindPersonByEmail(ctx, e); err == nil && p != nil {
				_, _ = c.DeleteContact(ctx, onlyoffice.ContactID(p))
			}
		})
	}
	// A — пустой about (как импорт #85); B — ручной about (не перезаписываем)
	createExisting := func(email, first, last, about string) {
		p, err := c.CreatePerson(ctx, first, last, 0, "", about)
		if err != nil {
			t.Fatalf("CreatePerson(%s): %v", email, err)
		}
		if _, err := c.AddContactInfo(ctx, onlyoffice.ContactID(p), "email", email, "Work", true); err != nil {
			t.Fatalf("AddContactInfo(%s): %v", email, err)
		}
	}
	createExisting(emailA, "Alice", "Backfill", "")
	createExisting(emailB, "Berta", "Backfill", "Manual note kept")

	plan, err := BuildPlan(&network.Manifest{
		Target: "alice@example.com",
		Source: "2dph graph mail+git (L-9.5 #234)",
		Links: []network.ManifestLink{
			{Person: emailA, Name: "Alice Backfill", Kind: "person",
				Msgs: 2, Threads: 1, Replies: 1, Period: "2026-08-01..2026-08-31",
				Premises: []network.Premise{{Kind: "mail", Ref: "bf-" + suffix, Date: "2026-08-01"}}},
			{Person: emailB, Name: "Berta Backfill", Kind: "person",
				Msgs: 1, Threads: 1, Replies: 0, Period: "2026-08-01..2026-08-02"},
		},
	}, tag)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(ctx, c, plan, Options{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Created != 0 || rep.Matched != 2 {
		t.Fatalf("Run report = %+v, want matched=2", rep)
	}

	// dry-run: Would=2, ничего не записано
	repBf, err := RunBackfill(ctx, c, plan, Options{DryRun: true})
	if err != nil {
		t.Fatalf("RunBackfill dry-run: %v", err)
	}
	if repBf.Updated != 0 || repBf.Would != 2 || repBf.Unchanged != 0 {
		t.Errorf("dry-run backfill report = %+v, want would=2", repBf)
	}

	// write: тег + about (только A; ручной about B не тронут)
	repBf, err = RunBackfill(ctx, c, plan, Options{})
	if err != nil {
		t.Fatalf("RunBackfill: %v", err)
	}
	if repBf.Updated != 2 || repBf.Failed != 0 || repBf.Missing != 0 {
		t.Errorf("backfill report = %+v, want updated=2", repBf)
	}
	pa, err := c.FindPersonByEmail(ctx, emailA)
	if err != nil || pa == nil {
		t.Fatalf("person A: %v", err)
	}
	fullA, err := c.GetContact(ctx, onlyoffice.ContactID(pa))
	if err != nil {
		t.Fatal(err)
	}
	if about := fmt.Sprint(fullA["about"]); !strings.Contains(about, "2 писем / 1 тредов / 1 ответов") ||
		!strings.Contains(about, "premises: mail bf-") || !strings.Contains(about, "source: 2dph graph mail+git") {
		t.Errorf("A about = %q, want premises-сводку", about)
	}
	if g, f := fmt.Sprint(fullA["firstName"]), fmt.Sprint(fullA["lastName"]); g != "Alice" || f != "Backfill" {
		t.Errorf("A name = %q/%q, want Alice/Backfill (поля не перезаписаны)", g, f)
	}
	pb, err := c.FindPersonByEmail(ctx, emailB)
	if err != nil || pb == nil {
		t.Fatalf("person B: %v", err)
	}
	fullB, err := c.GetContact(ctx, onlyoffice.ContactID(pb))
	if err != nil {
		t.Fatal(err)
	}
	if about := fmt.Sprint(fullB["about"]); about != "Manual note kept" {
		t.Errorf("B about = %q, want ручной текст сохранён (аддитивно)", about)
	}
	byTag, total, err := c.ListContactsByTag(ctx, tag, 50, 0)
	if err != nil {
		t.Fatalf("ListContactsByTag: %v", err)
	}
	ids := map[string]bool{}
	for _, row := range byTag {
		ids[onlyoffice.ContactID(row)] = true
	}
	if total < 2 || !ids[onlyoffice.ContactID(pa)] || !ids[onlyoffice.ContactID(pb)] {
		t.Errorf("по тегу %q total=%d ids=%v, want A и B", tag, total, ids)
	}

	// повтор: 0 изменений (тег+about уже есть / ручной about не трогаем)
	repBf, err = RunBackfill(ctx, c, plan, Options{})
	if err != nil {
		t.Fatalf("RunBackfill repeat: %v", err)
	}
	if repBf.Updated != 0 || repBf.Would != 0 || repBf.Unchanged != 2 {
		t.Errorf("repeat backfill report = %+v, want unchanged=2", repBf)
	}
}
