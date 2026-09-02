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
