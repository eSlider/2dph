package ooimport

// Офлайн-тесты маппинга CRM-манифеста сети → контакты OO (N-1.2 #269).
// cgo-free, синтетические фикстуры (Alice/Bob/example.com), без сети.
// Покрывают: ParseManifest, BuildPlan (имя/email/тег/about), фильтр
// сервисов (N-1.1 #268), сортировку по email.

import (
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/network"
	"gopkg.in/yaml.v3"
)

// fixtureManifest — манифест формата bin/network --accept-only (L-9.5 #234):
// person-линк с premises/extra, компания-ящик без display name,
// сервис-аккаунт (skip), линк без email (skip).
func fixtureManifest() *network.Manifest {
	return &network.Manifest{
		Target: "alice@example.com",
		Source: "2dph graph mail+git (L-9.5 #234)",
		Links: []network.ManifestLink{
			{
				Person: "Bob.Builder@Example.COM", Name: "Bob Builder", Kind: "person",
				Msgs: 12, Threads: 5, Replies: 3, Period: "2026-01-10..2026-06-30",
				Projects: []network.ManifestProject{{Repo: "demo", Period: "2026-03-01..2026-06-30"}},
				Premises: []network.Premise{
					{Kind: "mail", Ref: "m1@example.com", Date: "2026-01-10"},
					{Kind: "mail", Ref: "m2@example.com", Date: "2026-01-11"},
				},
				Extra: 3,
			},
			{
				Person: "gitlab@example.com", Name: "GitLab", Kind: "service",
				Msgs: 1089, Threads: 100, Replies: 0, Period: "2026-01-01..2026-08-31",
			},
			{
				Person: "info@wheregroup.example", Kind: "company",
				Msgs: 8, Threads: 4, Replies: 0, Period: "2026-02-01..2026-05-31",
			},
			{
				Person: "", Kind: "person",
				Msgs: 1, Threads: 1, Replies: 0, Period: "2026-03-01..2026-03-01",
			},
		},
	}
}

func TestParseManifest(t *testing.T) {
	raw := `target: alice@example.com
source: "2dph graph mail+git (L-9.5 #234)"
links:
  - person: bob.builder@example.com
    name: Bob Builder
    kind: person
    msgs: 12
    threads: 5
    replies: 3
    period: 2026-01-10..2026-06-30
    premises:
      - kind: mail
        ref: m1@example.com
        date: "2026-01-10"
      - kind: mail
        ref: m2@example.com
        date: "2026-01-11"
    extraPremises: 3
  - person: info@wheregroup.example
    kind: company
    msgs: 8
    threads: 4
    period: 2026-02-01..2026-05-31
    premises: []
`
	m, err := ParseManifest([]byte(raw))
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	if m.Target != "alice@example.com" {
		t.Errorf("Target = %q", m.Target)
	}
	if m.Source != "2dph graph mail+git (L-9.5 #234)" {
		t.Errorf("Source = %q", m.Source)
	}
	if len(m.Links) != 2 {
		t.Fatalf("len(links) = %d, want 2", len(m.Links))
	}
	first := m.Links[0]
	if first.Person != "bob.builder@example.com" || first.Name != "Bob Builder" || first.Kind != "person" {
		t.Errorf("first link = %+v", first)
	}
	if first.Msgs != 12 || first.Threads != 5 || first.Replies != 3 || first.Period != "2026-01-10..2026-06-30" {
		t.Errorf("first link counters = %+v", first)
	}
	if len(first.Premises) != 2 || first.Premises[0].Kind != "mail" || first.Premises[0].Ref != "m1@example.com" {
		t.Errorf("first link premises = %+v", first.Premises)
	}
	if first.Extra != 3 {
		t.Errorf("first link extraPremises = %d, want 3", first.Extra)
	}
	if got := m.Links[1].Person; got != "info@wheregroup.example" {
		t.Errorf("second link person = %q", got)
	}
}

func TestParseManifestBadYAML(t *testing.T) {
	if _, err := ParseManifest([]byte("target: [unclosed")); err == nil {
		t.Fatal("expected error for broken YAML")
	}
}

// TestManifestProducerConsumerRoundtrip — контракт продюсера (bin/network,
// yaml.Marshal network.Manifest) читается консьюмером (ParseManifest) без
// потери полей: yaml-теги типов (включая Premise с json-тегами) симметричны.
func TestManifestProducerConsumerRoundtrip(t *testing.T) {
	m := fixtureManifest()
	data, err := yaml.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, err := ParseManifest(data)
	if err != nil {
		t.Fatalf("parse producer output: %v", err)
	}
	if got.Target != m.Target || got.Source != m.Source || len(got.Links) != len(m.Links) {
		t.Fatalf("roundtrip header mismatch: %+v vs %+v", got, m)
	}
	bob := m.Links[0]
	gb := got.Links[0]
	if gb.Person != bob.Person || gb.Name != bob.Name || gb.Kind != bob.Kind ||
		gb.Msgs != bob.Msgs || gb.Threads != bob.Threads || gb.Replies != bob.Replies ||
		gb.Period != bob.Period || gb.Extra != bob.Extra || len(gb.Premises) != len(bob.Premises) {
		t.Fatalf("roundtrip link mismatch: %+v vs %+v", gb, bob)
	}
	if gb.Premises[0].Kind != bob.Premises[0].Kind || gb.Premises[0].Ref != bob.Premises[0].Ref {
		t.Fatalf("roundtrip premise mismatch: %+v vs %+v", gb.Premises[0], bob.Premises[0])
	}
	if len(gb.Projects) != 1 || gb.Projects[0].Repo != "demo" {
		t.Fatalf("roundtrip projects mismatch: %+v", gb.Projects)
	}
	// и план из продюсерского вывода совпадает с планом из исходника
	p1, err := BuildPlan(m, "")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := BuildPlan(got, "")
	if err != nil {
		t.Fatal(err)
	}
	if p1.Tag != p2.Tag || len(p1.Contacts) != len(p2.Contacts) ||
		p1.Contacts[0].About != p2.Contacts[0].About {
		t.Fatalf("plan from roundtrip differs: %+v vs %+v", p2, p1)
	}
}

func TestBuildPlanMapsPersonLink(t *testing.T) {
	p, err := BuildPlan(fixtureManifest(), "")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if len(p.Contacts) != 2 {
		t.Fatalf("len(contacts) = %d, want 2 (person+company; service и пустой email пропущены)", len(p.Contacts))
	}
	if p.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", p.Skipped)
	}
	// сортировка по email (детерминированный --limit)
	if p.Contacts[0].Email != "bob.builder@example.com" || p.Contacts[1].Email != "info@wheregroup.example" {
		t.Errorf("contacts order = %q, %q", p.Contacts[0].Email, p.Contacts[1].Email)
	}
	// Kind связи сохраняется (компания-группировка N-1.4 #271 по kind=company)
	if p.Contacts[0].Kind != "person" || p.Contacts[1].Kind != "company" {
		t.Errorf("contact kinds = %q, %q; want person, company", p.Contacts[0].Kind, p.Contacts[1].Kind)
	}
	bob := p.Contacts[0]
	if bob.Email != "bob.builder@example.com" {
		t.Errorf("bob email = %q (должен быть lowercase)", bob.Email)
	}
	if bob.Given != "Bob" || bob.Family != "Builder" {
		t.Errorf("bob name = %q/%q, want Bob/Builder", bob.Given, bob.Family)
	}
	wantAbout := "12 писем / 5 тредов / 3 ответов, период 2026-01-10..2026-06-30; " +
		"premises: mail m1@example.com; mail m2@example.com; +3 ещё; " +
		"source: 2dph graph mail+git (L-9.5 #234)"
	if bob.About != wantAbout {
		t.Errorf("bob about:\n got %q\nwant %q", bob.About, wantAbout)
	}
}

func TestBuildPlanRoleBoxFallbackName(t *testing.T) {
	p, err := BuildPlan(fixtureManifest(), "")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	// роль-ящик без display name: given из локальной части email
	info := p.Contacts[1]
	if info.Given != "Info" || info.Family != "" {
		t.Errorf("info name = %q/%q, want Info/\"\" (fallback local part)", info.Given, info.Family)
	}
	if !strings.Contains(info.About, "8 писем / 4 тредов / 0 ответов, период 2026-02-01..2026-05-31") {
		t.Errorf("info about = %q", info.About)
	}
}

func TestBuildPlanTagDefaultAndOverride(t *testing.T) {
	p, err := BuildPlan(fixtureManifest(), "")
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if p.Tag != "2dph:network:alice@example.com" {
		t.Errorf("default tag = %q, want 2dph:network:alice@example.com", p.Tag)
	}
	p2, err := BuildPlan(fixtureManifest(), "marketing:wheregroup")
	if err != nil {
		t.Fatalf("BuildPlan override: %v", err)
	}
	if p2.Tag != "marketing:wheregroup" {
		t.Errorf("override tag = %q", p2.Tag)
	}
}

func TestBuildPlanNeedsTag(t *testing.T) {
	m := fixtureManifest()
	m.Target = ""
	if _, err := BuildPlan(m, ""); err == nil {
		t.Fatal("expected error: tag required when manifest target empty and no --tag")
	}
	if _, err := BuildPlan(m, "2dph:network:x"); err != nil {
		t.Fatalf("tag override should satisfy: %v", err)
	}
}

func TestBuildPlanSortsByEmailNotManifestOrder(t *testing.T) {
	m := &network.Manifest{
		Target: "alice@example.com",
		Links: []network.ManifestLink{
			{Person: "zeta@example.com", Name: "Zeta Z.", Kind: "person", Msgs: 2, Threads: 1, Replies: 0, Period: "2026-01-01..2026-01-02"},
			{Person: "alpha@example.com", Name: "Alpha A.", Kind: "person", Msgs: 3, Threads: 1, Replies: 0, Period: "2026-01-01..2026-01-03"},
		},
	}
	p, err := BuildPlan(m, "t")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Contacts) != 2 || p.Contacts[0].Email != "alpha@example.com" || p.Contacts[1].Email != "zeta@example.com" {
		t.Errorf("contacts = %+v, want sorted alpha,zeta", p.Contacts)
	}
}

func TestBuildPlanNoPremisesNoSource(t *testing.T) {
	m := &network.Manifest{
		Target: "alice@example.com",
		Links: []network.ManifestLink{
			{Person: "bob@example.com", Kind: "person", Msgs: 2, Threads: 1, Replies: 0, Period: "2026-01-01..2026-01-02"},
		},
	}
	p, err := BuildPlan(m, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Contacts) != 1 {
		t.Fatalf("contacts = %d", len(p.Contacts))
	}
	if got := p.Contacts[0].About; got != "2 писем / 1 тредов / 0 ответов, период 2026-01-01..2026-01-02" {
		t.Errorf("about = %q (premises/source пустые — секций нет)", got)
	}
}
