package ooimport

// Офлайн-тесты компании-группировки role-ящиков по домену (N-1.4 #271):
// ParseCompanies (YAML домен→компания), EmailDomain, GroupCompanyLinks
// (какие company-kind контакты манифеста линкуются на компанию; person-kind
// не трогаем; company-kind без правила — unmapped). cgo-free, синтетика
// (Alice/Bob/wheregroup.example — без реальных адресов).

import (
	"reflect"
	"testing"

	"github.com/eSlider/2dph/internal/mailconv"
)

// fixtureCompanyContacts — role-ящики пилота (kind=company: alle/entwicklung/
// info/wartung на домене, events@suse вне маппинга) + реальные люди
// (kind=person, гдеgroup-домен — НЕ role-ящик, не линкуем).
func fixtureCompanyContacts() []Contact {
	return []Contact{
		{Email: "info@wheregroup.example", Kind: mailconv.KindCompany},
		{Email: "wartung@wheregroup.example", Kind: mailconv.KindCompany},
		{Email: "entwicklung@wheregroup.example", Kind: mailconv.KindCompany},
		{Email: "alle@wheregroup.example", Kind: mailconv.KindCompany},
		{Email: "events@suse.example", Kind: mailconv.KindCompany},
		{Email: "astrid.emde@wheregroup.example", Kind: mailconv.KindPerson},
		{Email: "bob@example.com", Kind: mailconv.KindPerson},
	}
}

func TestEmailDomain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"info@wheregroup.com", "wheregroup.com"},
		{"INFO@WhereGroup.COM", "wheregroup.com"},
		{" alle@wheregroup.com ", "wheregroup.com"},
		{"x@sub.wheregroup.com", "sub.wheregroup.com"},
		{"no-at", ""},
		{"a@b", "b"},
		{"trailing@", ""},
	}
	for _, c := range cases {
		if got := EmailDomain(c.in); got != c.want {
			t.Errorf("EmailDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGroupCompanyLinksRoleBoxesByDomain(t *testing.T) {
	rules := []CompanyRule{{Domain: "wheregroup.example", Name: "WhereGroup"}}
	links, unmapped := GroupCompanyLinks(fixtureCompanyContacts(), rules)
	if unmapped != 1 {
		t.Errorf("unmapped = %d, want 1 (events@suse.example — company без правила)", unmapped)
	}
	if len(links) != 1 {
		t.Fatalf("links = %d, want 1", len(links))
	}
	l := links[0]
	if l.Rule != rules[0] {
		t.Errorf("link rule = %+v, want %+v", l.Rule, rules[0])
	}
	wantEmails := []string{
		"alle@wheregroup.example",
		"entwicklung@wheregroup.example",
		"info@wheregroup.example",
		"wartung@wheregroup.example",
	}
	if !reflect.DeepEqual(l.Emails, wantEmails) {
		t.Errorf("link emails = %v, want %v (сорт; person-kind не включён)", l.Emails, wantEmails)
	}
}

func TestGroupCompanyLinksNoCompanyKindIgnoredWithoutRules(t *testing.T) {
	links, unmapped := GroupCompanyLinks(fixtureCompanyContacts(), nil)
	if len(links) != 0 {
		t.Errorf("links = %+v, want none", links)
	}
	if unmapped != 5 {
		t.Errorf("unmapped = %d, want 5 (все company-kind без правил)", unmapped)
	}
}

func TestGroupCompanyLinksTwoDomainsOneCompany(t *testing.T) {
	// два домена → одна компания (имя компании одно): общий CompanyLink
	contacts := []Contact{
		{Email: "info@wheregroup.example", Kind: mailconv.KindCompany},
		{Email: "info@wheregroup.de", Kind: mailconv.KindCompany},
	}
	rules := []CompanyRule{
		{Domain: "wheregroup.example", Name: "WhereGroup"},
		{Domain: "wheregroup.de", Name: "WhereGroup"},
	}
	links, unmapped := GroupCompanyLinks(contacts, rules)
	if unmapped != 0 || len(links) != 1 {
		t.Fatalf("links=%d unmapped=%d, want 1/0", len(links), unmapped)
	}
	want := []string{"info@wheregroup.de", "info@wheregroup.example"}
	if !reflect.DeepEqual(links[0].Emails, want) {
		t.Errorf("emails = %v, want %v", links[0].Emails, want)
	}
}

func TestGroupCompanyLinksDeterministicOrder(t *testing.T) {
	rules := []CompanyRule{
		{Domain: "zebra.example", Name: "Zebra"},
		{Domain: "alpha.example", Name: "Alpha"},
	}
	contacts := []Contact{
		{Email: "x@zebra.example", Kind: mailconv.KindCompany},
		{Email: "y@alpha.example", Kind: mailconv.KindCompany},
	}
	links, _ := GroupCompanyLinks(contacts, rules)
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2", len(links))
	}
	if links[0].Rule.Name != "Alpha" || links[1].Rule.Name != "Zebra" {
		t.Errorf("links order = %q,%q; want Alpha,Zebra (сорт по имени компании)",
			links[0].Rule.Name, links[1].Rule.Name)
	}
}

func TestParseCompanies(t *testing.T) {
	raw := []byte("wheregroup.com: WhereGroup\nsuse.com: SUSE\n")
	rules, err := ParseCompanies(raw)
	if err != nil {
		t.Fatalf("ParseCompanies: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(rules))
	}
	if rules[0].Domain != "suse.com" || rules[0].Name != "SUSE" {
		t.Errorf("rules[0] = %+v, want suse.com/SUSE (сорт по домену)", rules[0])
	}
	if rules[1].Domain != "wheregroup.com" || rules[1].Name != "WhereGroup" {
		t.Errorf("rules[1] = %+v", rules[1])
	}
}

func TestParseCompaniesSortsAndTrims(t *testing.T) {
	rules, err := ParseCompanies([]byte("z.example: Zebra\na.example:  Alpha \n"))
	if err != nil {
		t.Fatalf("ParseCompanies: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(rules))
	}
	if rules[0].Domain != "a.example" || rules[0].Name != "Alpha" || rules[1].Domain != "z.example" {
		t.Errorf("rules = %+v, want trimmed a.example→Alpha, z.example→Zebra", rules)
	}
}

func TestParseCompaniesBad(t *testing.T) {
	if _, err := ParseCompanies([]byte("- just\n- a\n- list\n")); err == nil {
		t.Error("expected error for non-map YAML")
	}
	if _, err := ParseCompanies([]byte(": nope\n")); err == nil {
		t.Error("expected error for empty domain")
	}
	if _, err := ParseCompanies([]byte("nodot: A\n")); err == nil {
		t.Error("expected error for domain without dot")
	}
	if _, err := ParseCompanies([]byte("info@wheregroup.com: A\n")); err == nil {
		t.Error("expected error for email-shaped key (ключ — домен, не email)")
	}
	if _, err := ParseCompanies([]byte("domain.com:  \n")); err == nil {
		t.Error("expected error for empty company name")
	}
	if _, err := ParseCompanies([]byte("x.com: A\nx.com: B\n")); err == nil {
		t.Error("expected error for duplicate domain")
	}
	if rules, err := ParseCompanies([]byte("")); err != nil || len(rules) != 0 {
		t.Errorf("empty doc: rules=%v err=%v, want 0/nil", rules, err)
	}
}
