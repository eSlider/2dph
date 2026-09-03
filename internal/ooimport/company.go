package ooimport

// Компания-группировка role-ящиков по домену email (N-1.4 #271): маппинг
// «домен → компания» (пилот: wheregroup.com), FindCompany/CreateCompany
// (idempotent по нормализованному имени, go-onlyoffice) + UpdatePerson с
// companyId для линковки. Линкуются только company-kind контакты манифеста
// (роль-ящики/организации: alle@, info@, wartung@…); person-kind не трогаем
// (реальные люди — не «роль-ящик»; их привязка к компании — отдельное
// решение владельца). company-kind без правила — не линкуется (unmapped),
// остаётся Person-контактом как есть (events@suse в пилоте).
//
// Idempotent: повтор = 0 изменений (компания найдена, персоны уже на ней).

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/eslider/go-onlyoffice"
	"gopkg.in/yaml.v3"
)

// CompanyRule — маппинг «домен email → компания»: company-kind контакты
// домена линкуются на компанию с display name Name (FindCompany/
// CreateCompany идемпотентны по нормализованному имени).
type CompanyRule struct {
	Domain string
	Name   string
}

// CompanyLink — компания и role-ящики (emails kind=company её домена),
// которых линкуем на FindCompany/CreateCompany + UpdatePerson.
type CompanyLink struct {
	Rule   CompanyRule
	Emails []string // sorted, lowercase
}

// EmailDomain возвращает доменную часть email (lowercase, без '@'), или "",
// если email без '@'.
func EmailDomain(email string) string {
	e := strings.ToLower(strings.TrimSpace(email))
	i := strings.LastIndexByte(e, '@')
	if i < 0 || i == len(e)-1 {
		return ""
	}
	return e[i+1:]
}

// ParseCompanies читает YAML-маппинг домен → компания (файл --companies),
// напр. "wheregroup.com: WhereGroup". Ключи/значения триммятся; ключ
// нормализуется в lowercase; домен должен содержать '.' и не быть email
// (ключ — домен, не адрес); пустые значения и повторы домена — ошибка.
// Правила отсортированы по домену — детерминированный порядок прогона.
func ParseCompanies(data []byte) ([]CompanyRule, error) {
	var raw map[string]string
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse companies: %w", err)
	}
	rules := make([]CompanyRule, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	domains := make([]string, 0, len(raw))
	for d := range raw {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	for _, d := range domains {
		domain := strings.ToLower(strings.TrimSpace(d))
		name := strings.TrimSpace(raw[d])
		if domain == "" {
			return nil, fmt.Errorf("parse companies: empty domain key %q", d)
		}
		if strings.Contains(domain, "@") || !strings.Contains(domain, ".") {
			return nil, fmt.Errorf("parse companies: %q — ключ должен быть доменом (не email, с точкой)", domain)
		}
		if name == "" {
			return nil, fmt.Errorf("parse companies: domain %q has empty company name", domain)
		}
		if seen[domain] {
			return nil, fmt.Errorf("parse companies: duplicate domain %q", domain)
		}
		seen[domain] = true
		rules = append(rules, CompanyRule{Domain: domain, Name: name})
	}
	return rules, nil
}

// GroupCompanyLinks решает, какие company-kind контакты плана (роль-ящики/
// организации манифеста) линкуются на компанию по домену правила. person-kind
// не трогаем (реальные люди — не роль-ящик); company-kind без подходящего
// правила не линкуется и считается в unmapped. Детерминировано: правила —
// по имени компании, emails — по возрастанию; два домена с одной компанией
// схлопываются в один CompanyLink.
func GroupCompanyLinks(contacts []Contact, rules []CompanyRule) (links []CompanyLink, unmapped int) {
	ruleByDomain := make(map[string]CompanyRule, len(rules))
	for _, r := range rules {
		ruleByDomain[r.Domain] = r
	}
	byName := make(map[string]*CompanyLink)
	var order []string
	for _, ct := range contacts {
		if ct.Kind != mailconv.KindCompany {
			continue
		}
		r, ok := ruleByDomain[EmailDomain(ct.Email)]
		if !ok {
			unmapped++
			continue
		}
		l, seen := byName[r.Name]
		if !seen {
			l = &CompanyLink{Rule: r}
			byName[r.Name] = l
			order = append(order, r.Name)
		}
		l.Emails = append(l.Emails, ct.Email)
	}
	sort.Strings(order)
	links = make([]CompanyLink, 0, len(order))
	for _, name := range order {
		l := byName[name]
		sort.Strings(l.Emails)
		links = append(links, *l)
	}
	return links, unmapped
}

// CompanyReport — итоги компании-группировки role-ящиков.
type CompanyReport struct {
	CompaniesFound   int // компаний уже было в CRM
	CompaniesCreated int // создано в этом прогоне (write)
	CompaniesMissing int // компания не найдена (write: была бы создана; dry-run: создастся)
	Linked           int // персон слинковано UpdatePerson с companyId (write)
	WouldLink        int // dry-run: персон было бы слинковано
	AlreadyLinked    int // персон уже на этой компании (повтор = все)
	NotFound         int // Person по email нет в CRM (role-ящик ещё не создан)
	Unmapped         int // company-kind контакты без правила (не линкуются)
	Failed           int // сбои (компания/линковка)
	Failures         []Failure
}

// RunCompanies линкует role-ящики плана на компанию по домену правил:
// для каждой компании FindCompany → если нет и не dry-run, CreateCompany;
// для каждого role-ящика (email kind=company домена правила, есть в CRM) —
// GetContact (имя/текущая компания) → UpdatePerson с companyId, если ещё не
// на этой компании. Dry-run: только lookups, ничего не пишет (WouldLink).
// Идемпотентно: повтор = CompaniesFound, AlreadyLinked — 0 изменений.
func RunCompanies(ctx context.Context, c *onlyoffice.Client, contacts []Contact, rules []CompanyRule, opts Options) (CompanyReport, error) {
	var rep CompanyReport
	links, unmapped := GroupCompanyLinks(contacts, rules)
	rep.Unmapped = unmapped
	if len(links) == 0 {
		return rep, nil
	}
	idx, err := c.BuildContactEmailIndex(ctx)
	if err != nil {
		return rep, fmt.Errorf("contact index: %w", err)
	}
	for _, l := range links {
		company, err := c.FindCompany(ctx, l.Rule.Name)
		if err != nil {
			rep.Failed++
			rep.Failures = append(rep.Failures, Failure{Email: l.Rule.Name, Err: fmt.Sprintf("find company: %v", err)})
			continue
		}
		companyID := 0
		switch {
		case company != nil:
			rep.CompaniesFound++
			companyID, err = companyIDInt(company)
			if err != nil {
				rep.Failed++
				rep.Failures = append(rep.Failures, Failure{Email: l.Rule.Name, Err: err.Error()})
				continue
			}
		case opts.DryRun:
			rep.CompaniesMissing++
		default:
			company, err = c.CreateCompany(ctx, l.Rule.Name)
			if err != nil {
				rep.Failed++
				rep.Failures = append(rep.Failures, Failure{Email: l.Rule.Name, Err: fmt.Sprintf("create company: %v", err)})
				continue
			}
			rep.CompaniesCreated++
			companyID, err = companyIDInt(company)
			if err != nil {
				rep.Failed++
				rep.Failures = append(rep.Failures, Failure{Email: l.Rule.Name, Err: err.Error()})
				continue
			}
		}
		for _, email := range l.Emails {
			id, ok := idx[email]
			if !ok {
				rep.NotFound++
				continue
			}
			full, err := c.GetContact(ctx, id)
			if err != nil {
				rep.Failed++
				rep.Failures = append(rep.Failures, Failure{Email: email, Err: fmt.Sprintf("get contact: %v", err)})
				continue
			}
			if companyID == 0 { // dry-run, компании в CRM нет — id для сверки неизвестен
				rep.WouldLink++
				continue
			}
			if curCompanyID(full) == companyID {
				rep.AlreadyLinked++
				continue
			}
			if opts.DryRun {
				rep.WouldLink++
				continue
			}
			first := strings.TrimSpace(fmt.Sprint(full["firstName"]))
			last := strings.TrimSpace(fmt.Sprint(full["lastName"]))
			if _, err := c.UpdatePerson(ctx, id, first, last, companyID, "", ""); err != nil {
				rep.Failed++
				rep.Failures = append(rep.Failures, Failure{Email: email, Err: fmt.Sprintf("link company: %v", err)})
				continue
			}
			rep.Linked++
		}
	}
	return rep, nil
}

// companyIDInt достаёт числовой id компании из строки-ответа OO.
func companyIDInt(company map[string]any) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(company["id"])))
	if err != nil {
		return 0, fmt.Errorf("company id %q: %w", company["id"], err)
	}
	return id, nil
}

// curCompanyID возвращает id компании, к которой привязан Person
// (GetContact: вложенный объект "company"; 0 — нет компании).
func curCompanyID(person map[string]any) int {
	co, ok := person["company"].(map[string]any)
	if !ok {
		return 0
	}
	id, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(co["id"])))
	return id
}
