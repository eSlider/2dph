// Package ooimport — коннектор сети связей → OnlyOffice CRM (N-1.2 #269,
// epic #267). Читает CRM-манифест accept-связей (network.Manifest — выход
// bin/network --accept-only, L-9.5 #234) и пишет каждую не-сервисную связь
// как контакт Person: имя (given/family из display name, fallback — локальная
// часть email), email (AddContactInfo primary), тег источника и about со
// сводкой msgs/threads/replies/period + premises-ссылками на Message/Commit.
//
// Маппинг (решение PO/владельца, тело #269 + epic #267):
//
//   - каждая accept-связь → контакт Person: «деловая связь» в OO CRM =
//     существование контакта с email + тегом источника (нативной записи
//     Person↔Person в OO нет — не выдумываем); сила и premises — в about и
//     теге; полный манифест — артефакт вне CRM;
//   - kind=company (роль-ящики info@/alle@, организации) тоже становится
//     Person-контактом, а компания-группировка role-ящиков по домену —
//     отдельным шагом RunCompanies (N-1.4 #271); linkedin-релеи людей
//     (hit-reply@linkedin.com с реальным display name) — Person как есть:
//     email = релей, имя = реальный человек;
//   - kind=service (N-1.1 #268: GitLab/PayPal/LinkedIn/markets-platform/
//     трекеры/рассылки) пропускается — в CRM не идёт;
//   - идемпотентность: ключ = email (lowercase); существующие по email
//     (включая импорт VCF/MAB #85) не перезаписываются — skip при создании,
//     аддитивное дописывание тега/about — RunBackfill (N-1.4 #271).
//
// cgo-free; тесты офлайн (план/чистая логика) + интеграция с живым OO под
// //go:build integration (creds ONLYOFFICE_URL/USER/PASS, без creds — skip).
package ooimport

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/eSlider/2dph/internal/network"
	"github.com/eslider/go-onlyoffice"
	"gopkg.in/yaml.v3"
)

// ParseManifest декодирует CRM-манифест accept-связей (YAML, формат
// bin/network --accept-only).
func ParseManifest(data []byte) (*network.Manifest, error) {
	var m network.Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

// Contact — один запланированный CRM-контакт (Person) для связи сети.
type Contact struct {
	Email  string // ключ идемпотентности (lowercase)
	Given  string
	Family string
	About  string
	Kind   string // класс связи манифеста: person|company (kind=service отфильтрован; пусто = person)
}

// Plan — результат маппинга манифеста: готовые кандидаты + тег источника.
type Plan struct {
	Target   string
	Tag      string
	Contacts []Contact // отсортированы по Email (детерминированный --limit)
	Skipped  int       // линки, исключённые из импорта (service / без email)
}

// BuildPlan маппит манифест сети в контакты OO. tagOverride — тег источника;
// пустой → "2dph:network:<target-email>". Линки kind=service (N-1.1 #268) и
// без email пропускаются (Skipped). Имя: mailconv.SplitPersonName (fallback —
// локальная часть email). About: сводка из манифеста (ничего не выдумываем).
func BuildPlan(m *network.Manifest, tagOverride string) (Plan, error) {
	if m == nil {
		return Plan{}, errors.New("nil manifest")
	}
	tag := strings.ToLower(strings.TrimSpace(tagOverride))
	if tag == "" {
		tag = "2dph:network:" + strings.ToLower(strings.TrimSpace(m.Target))
	}
	if tag == "2dph:network:" {
		return Plan{}, errors.New("source tag required: pass --tag or set manifest target")
	}
	p := Plan{Target: m.Target, Tag: tag}
	for _, l := range m.Links {
		email := strings.ToLower(strings.TrimSpace(l.Person))
		if l.Kind == mailconv.KindService || email == "" {
			p.Skipped++
			continue
		}
		given, family := mailconv.SplitPersonName(l.Name, email)
		p.Contacts = append(p.Contacts, Contact{
			Email:  email,
			Given:  given,
			Family: family,
			About:  aboutText(&l, m.Source),
			Kind:   l.Kind,
		})
	}
	sort.Slice(p.Contacts, func(i, j int) bool { return p.Contacts[i].Email < p.Contacts[j].Email })
	return p, nil
}

// aboutText — краткая сводка связи для поля about: счётчики и период,
// premises (первые 5 из манифеста + extraPremises), источник — всё текстом
// из манифеста.
func aboutText(l *network.ManifestLink, source string) string {
	b := fmt.Sprintf("%d писем / %d тредов / %d ответов, период %s",
		l.Msgs, l.Threads, l.Replies, l.Period)
	var parts []string
	for _, pr := range l.Premises {
		parts = append(parts, pr.Kind+" "+pr.Ref)
	}
	if l.Extra > 0 {
		parts = append(parts, fmt.Sprintf("+%d ещё", l.Extra))
	}
	if len(parts) > 0 {
		b += "; premises: " + strings.Join(parts, "; ")
	}
	if s := strings.TrimSpace(source); s != "" {
		b += "; source: " + s
	}
	return b
}

// Options — настройки прогона Run.
type Options struct {
	DryRun bool // report only: lookups, ничего не пишет
	Limit  int  // max созданий за прогон (0 = все)
}

// Report — итоги прогона Run.
type Report struct {
	Created  int       // контактов создано в CRM (dry-run: всегда 0)
	Matched  int       // существующие по email — пропущены (не перезаписаны)
	Skipped  int       // service / без email — не кандидаты
	Failed   int       // сбой создания (созданный контакт откатан — см. createContact)
	Pending  int       // новые, не созданные из-за Limit
	New      int       // новых кандидатов всего (dry-run: сколько было бы создано)
	Failures []Failure // детали сбоев (email + причина)
}

// Failure — один сбой создания контакта.
type Failure struct {
	Email string
	Err   string
}

// Run выполняет прогон: один проход по контактам CRM (BuildContactEmailIndex,
// lowercase email), кандидаты разбиваются на matched (есть) и новые;
// DryRun — только отчёт; иначе создаются новые контакты
// (CreatePerson + AddContactInfo email primary + тег источника). Идемпотентно:
// повторный прогон после успеха = 0 созданий, все matched.
func Run(ctx context.Context, c *onlyoffice.Client, p Plan, opts Options) (Report, error) {
	rep := Report{Skipped: p.Skipped}
	if len(p.Contacts) == 0 {
		return rep, nil
	}
	idx, err := c.BuildContactEmailIndex(ctx)
	if err != nil {
		return rep, fmt.Errorf("contact index: %w", err)
	}
	existing := make(map[string]bool, len(idx))
	for e := range idx {
		existing[e] = true
	}
	fresh, matched := splitByExisting(p.Contacts, existing)
	rep.Matched = matched
	rep.New = len(fresh)
	if opts.DryRun || len(fresh) == 0 {
		return rep, nil
	}
	if err := c.CreateContactTag(ctx, p.Tag); err != nil {
		return rep, fmt.Errorf("create tag %q: %w", p.Tag, err)
	}
	for _, ct := range fresh {
		if opts.Limit > 0 && rep.Created >= opts.Limit {
			rep.Pending++
			continue
		}
		if err := createContact(ctx, c, ct, p.Tag); err != nil {
			rep.Failed++
			rep.Failures = append(rep.Failures, Failure{Email: ct.Email, Err: err.Error()})
			continue
		}
		rep.Created++
	}
	return rep, nil
}

// splitByExisting делит кандидатов на новых и уже присутствующих в CRM
// (ключи existing — lowercase email, нормализованы BuildContactEmailIndex).
func splitByExisting(contacts []Contact, existing map[string]bool) (fresh []Contact, matched int) {
	for _, ct := range contacts {
		if existing[ct.Email] {
			matched++
			continue
		}
		fresh = append(fresh, ct)
	}
	return fresh, matched
}

// createContact создаёт Person (given/family, about), прикрепляет email
// (primary, Work) и тег источника. При сбое email/тег-прикрепления удаляет
// только что созданный контакт (rollback): контакт без email не находится
// повторным прогоном и привёл бы к дублю — идемпотентность держится на
// том, что «created» = полностью созданный контакт.
func createContact(ctx context.Context, c *onlyoffice.Client, ct Contact, tag string) error {
	person, err := c.CreatePerson(ctx, ct.Given, ct.Family, 0, "", ct.About)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	id := fmt.Sprint(person["id"])
	if _, err := c.AddContactInfo(ctx, id, "email", ct.Email, "Work", true); err != nil {
		_, _ = c.DeleteContact(ctx, id)
		return fmt.Errorf("email: %w", err)
	}
	if err := c.AddContactTag(ctx, id, tag); err != nil {
		_, _ = c.DeleteContact(ctx, id)
		return fmt.Errorf("tag: %w", err)
	}
	return nil
}
