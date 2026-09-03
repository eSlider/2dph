// Package network — сеть связей L-9.5 (#234): «с кем и через кого» из
// mail+commits поверх готового графа (D-1 #257: Message/Person/рёбра;
// L-9.4 #233: Commit/Person/AUTHORED). Чистая логика без liblbug: вход —
// строки выборки из графа (MsgRow/CommitRow/PersonRow), выход —
// детерминированные связи Person↔Person с premises (Message.id/Commit.id) и
// verdict (accept/weaken) по audit-картам Vinogradov (sufficient reason).
// Исполнение запросов к живой Ladybug — bin/network/network.go (cgo,
// read-only), по образцу bin/git/facts.go.
//
// Деривации (только из существующих узлов/рёбер, ничего не выдумываем):
//
//	mail  — письмо, где target — отправитель и Q — получатель (или
//	        наоборот): прямой контакт, вес роли TO=1.0 / CC=0.5 / BCC=0.25;
//	        общий получатель (оба в To/CC/BCC, sender третий) — слабее;
//	        REPLY_TO-цепь (письмо Q отвечает на письмо target) — диалог;
//	        общие треды (thread_id) — контекст.
//	git   — общий проект: target и Q оба AUTHORED коммиты в один
//	        Commit.repo (AUTHORED, L-9.4 #233); premises — commit id.
//
// Вердикт (ADR-0012 §сеть связей п.2/п.4): одиночный контакт (1 письмо без
// ответа и без проекта) — weaken + gap OPEN, в CRM-экспорт не идёт;
// ≥2 прямых писем / диалог REPLY_TO / общий проект — accept.
package network

import (
	"sort"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/mailconv"
)

// MsgRow — одно письмо графа (Message-узел + роли участников).
// ReplyTo — id сообщения, на которое отвечает это письмо (REPLY_TO, "" если
// нет). SentAt — RFC3339 (как в графе).
type MsgRow struct {
	ID       string
	ThreadID string
	SentAt   string
	GatorRef string
	Sender   string   // Person.id = email отправителя
	To       []string // Person.id получателей TO
	CC       []string // Person.id получателей CC
	BCC      []string // Person.id получателей BCC
	ReplyTo  string   // Message.id родителя по REPLY_TO
}

// CommitRow — git-коммит (Commit-узел + AUTHORED на Person).
type CommitRow struct {
	ID    string // repo:sha
	Repo  string
	Date  string // RFC3339
	Email string // Person.id автора (lowercase)
}

// PersonRow — Person-узел графа (id = email).
type PersonRow struct {
	ID   string
	Name string
}

// Rows — срез строк выборки графа (всё, что нужно сети).
type Rows struct {
	Msgs    []MsgRow
	Commits []CommitRow
	Persons []PersonRow
}

// Filter — фильтры сети: целевой Person (email), проект (repo — git-ось),
// период (Since/Until, включая границы, по sent_at/commit date).
type Filter struct {
	Person  string
	Project string
	Since   time.Time
	Until   time.Time
}

// Premise — одно свидетельство связи (sufficient reason): ссылка на
// Message или Commit из графа.
type Premise struct {
	Kind string `json:"kind"` // "mail" | "commit"
	Ref  string `json:"ref"`  // Message.id | Commit.id (repo:sha)
	Date string `json:"date,omitempty"`
}

// ProjectLink — общий проект: target и Q оба AUTHORED в один repo.
type ProjectLink struct {
	Repo      string `json:"repo"`
	CommitsMe int    `json:"commitsMe"`
	CommitsQ  int    `json:"commitsQ"`
	Period    string `json:"period"` // по коммитам обеих сторон
}

// Link — одна связь target↔Q (агрегат mail+git каналов).
type Link struct {
	Person   string        `json:"person"` // email Q
	Name     string        `json:"name"`
	Kind     string        `json:"kind"`     // person | company | service (N-1.1 #268)
	Msgs     int           `json:"msgs"`     // прямые письма (sender↔recipient)
	SharedCC int           `json:"sharedCC"` // письма, где оба — получатели
	Threads  int           `json:"threads"`  // треды с общими письмами
	Replies  int           `json:"replies"`  // REPLY_TO-рёбра между письмами
	Weight   float64       `json:"weight"`   // сила (вес ролей + диалог)
	Period   string        `json:"period"`
	Projects []ProjectLink `json:"projects,omitempty"`
	Premises []Premise     `json:"premises"`
	Verdict  string        `json:"verdict"` // accept | weaken
	Gaps     []string      `json:"gaps,omitempty"`
}

// Ролевые веса прямого контакта (тело #234: «сила … SENT/TO/CC вес»).
const (
	roleTO  = 1.0
	roleCC  = 0.5
	roleBCC = 0.25
	// бонусы: диалог REPLY_TO и общий получатель — слабее прямого TO
	replyBonus  = 0.5
	sharedBonus = 0.25
)

// BuildLinks строит сеть для целевого Person: ранжированные связи (вес
// убывает, tie-break email), детерминированные, с premises и verdict.
func BuildLinks(rows Rows, f Filter) []Link {
	target := strings.ToLower(strings.TrimSpace(f.Person))
	if target == "" {
		return nil
	}
	name := map[string]string{}
	for _, p := range rows.Persons {
		name[p.ID] = p.Name
	}

	// senderOf: Message.id → Person.id отправителя (SENT-ребро).
	senderOf := map[string]string{}
	for _, m := range rows.Msgs {
		if m.ID != "" {
			senderOf[m.ID] = strings.ToLower(m.Sender)
		}
	}

	// состояние связи target↔Q
	type acc struct {
		link *Link
		seen map[string]bool // premises mail (dedup по Message.id)
		tids map[string]bool // thread_id общих писем
	}
	accs := map[string]*acc{}
	order := []string{}
	get := func(email string) *acc {
		a := accs[email]
		if a == nil {
			a = &acc{link: &Link{Person: email, Name: name[email]},
				seen: map[string]bool{}, tids: map[string]bool{}}
			accs[email] = a
			order = append(order, email)
		}
		return a
	}
	noteDate := func(l *Link, rfc3339 string) {
		if d := dayOnly(rfc3339); d != "" {
			l.Period = spanJoin(l.Period, d)
		}
	}
	premiseMail := func(a *acc, m MsgRow) {
		if m.ID == "" || a.seen[m.ID] {
			return
		}
		a.seen[m.ID] = true
		a.link.Premises = append(a.link.Premises, Premise{Kind: "mail", Ref: m.ID, Date: dayOnly(m.SentAt)})
	}

	// --- mail-канал ---
	for _, m := range rows.Msgs {
		if !inPeriod(m.SentAt, f) {
			continue
		}
		sender := strings.ToLower(m.Sender)
		// роль target в письме (получатель?): 0 = не получатель
		roleTarget := recipientRole(m, target)
		direct := sender == target // письмо ОТ target

		// участники-получатели письма (все, кроме target), с ролями
		type rc struct {
			email string
			role  float64
		}
		var recips []rc
		seenRecip := map[string]bool{}
		addRecip := func(email string, role float64) {
			email = strings.ToLower(email)
			if email == "" || email == target || seenRecip[email] {
				return
			}
			seenRecip[email] = true
			recips = append(recips, rc{email, role})
		}
		for _, e := range m.To {
			addRecip(e, roleTO)
		}
		for _, e := range m.CC {
			addRecip(e, roleCC)
		}
		for _, e := range m.BCC {
			addRecip(e, roleBCC)
		}

		if direct {
			// письмо от target: каждый получатель — прямой контакт
			for _, r := range recips {
				a := get(r.email)
				l := a.link
				l.Msgs++
				l.Weight += r.role
				if m.ThreadID != "" {
					a.tids[m.ThreadID] = true
				}
				noteDate(l, m.SentAt)
				premiseMail(a, m)
			}
			continue
		}
		if sender == "" {
			continue // письмо без sender — участники не определены
		}
		if roleTarget == 0 {
			continue // target в письме не участвует — связи не порождает
		}
		// письмо от Q (sender) к target: Q — прямой контакт
		{
			a := get(sender)
			l := a.link
			l.Msgs++
			l.Weight += roleTarget // роль target в письме Q = сила контакта Q→target
			if m.ThreadID != "" {
				a.tids[m.ThreadID] = true
			}
			noteDate(l, m.SentAt)
			premiseMail(a, m)
		}
		// остальные получатели письма Q (не sender, не target) — общий
		// получатель с target
		for _, r := range recips {
			if r.email == sender {
				continue
			}
			a := get(r.email)
			l := a.link
			l.SharedCC++
			l.Weight += sharedBonus
			if m.ThreadID != "" {
				a.tids[m.ThreadID] = true
			}
			noteDate(l, m.SentAt)
			premiseMail(a, m)
		}
	}

	// --- REPLY_TO-диалоги: письмо Q отвечает на письмо target и наоборот ---
	for _, m := range rows.Msgs {
		if m.ReplyTo == "" || !inPeriod(m.SentAt, f) {
			continue
		}
		child := strings.ToLower(m.Sender)
		parent, ok := senderOf[m.ReplyTo]
		if !ok || parent == "" || child == "" {
			continue
		}
		var other string
		switch {
		case child == target && parent != target:
			other = parent // target ответил Q
		case parent == target && child != target:
			other = child // Q ответил target
		default:
			continue
		}
		a := get(other)
		l := a.link
		l.Replies++
		l.Weight += replyBonus
		if m.ThreadID != "" {
			a.tids[m.ThreadID] = true
		}
		noteDate(l, m.SentAt)
		premiseMail(a, m)
	}

	// треды: число thread_id писем, где target и Q оба участвуют
	for _, a := range accs {
		a.link.Threads = len(a.tids)
	}

	// --- git-ось: общий проект (AUTHORED Commit.repo) ---
	projByRepo := map[string][]CommitRow{}
	for _, c := range rows.Commits {
		if !inPeriod(c.Date, f) {
			continue
		}
		if strings.ToLower(c.Email) == "" {
			continue
		}
		projByRepo[c.Repo] = append(projByRepo[c.Repo], c)
	}
	for repo, cs := range projByRepo {
		me := 0
		var meDates []string
		qByEmail := map[string][]CommitRow{}
		var qOrder []string
		for _, c := range cs {
			email := strings.ToLower(c.Email)
			if email == target {
				me++
				meDates = append(meDates, c.Date)
				continue
			}
			if _, ok := qByEmail[email]; !ok {
				qOrder = append(qOrder, email)
			}
			qByEmail[email] = append(qByEmail[email], c)
		}
		if me == 0 {
			continue // target в этот repo не коммитил — связи через repo нет
		}
		for _, email := range qOrder {
			qs := qByEmail[email]
			a := get(email)
			l := a.link
			all := append(append([]string{}, meDates...), commitDates(qs)...)
			l.Projects = append(l.Projects, ProjectLink{
				Repo: repo, CommitsMe: me, CommitsQ: len(qs), Period: spanDays(all),
			})
			for _, c := range qs {
				l.Premises = append(l.Premises, Premise{Kind: "commit", Ref: c.ID, Date: dayOnly(c.Date)})
			}
		}
	}

	// сортировка проектов внутри связи по repo
	for _, a := range accs {
		sort.Slice(a.link.Projects, func(i, j int) bool {
			return a.link.Projects[i].Repo < a.link.Projects[j].Repo
		})
	}

	// вердикты + фильтры + сортировка вывода
	var out []Link
	for _, email := range order {
		l := accs[email].link
		if f.Project != "" {
			has := false
			for _, p := range l.Projects {
				if p.Repo == f.Project {
					has = true
					break
				}
			}
			if !has {
				continue
			}
		}
		l.Verdict, l.Gaps = verdict(l)
		l.Kind = mailconv.ClassifySender(mailconv.ParsedAddress{Name: l.Name, Email: l.Person})
		if l.Period == "" {
			for _, p := range l.Projects {
				l.Period = mergePeriods(l.Period, p.Period)
			}
		}
		out = append(out, *l)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Weight != out[j].Weight {
			return out[i].Weight > out[j].Weight
		}
		return out[i].Person < out[j].Person
	})
	return out
}

// verdict по audit-карте (ADR-0012 п.2/п.4): одиночный контакт без диалога и
// проекта — weaken + OPEN; устойчивая переписка/диалог/общий проект — accept.
func verdict(l *Link) (string, []string) {
	switch {
	case l.Msgs >= 2 || l.Replies >= 1 || len(l.Projects) > 0:
		return "accept", nil
	case l.Msgs == 1:
		return "weaken", []string{"OPEN: одиночный контакт (1 письмо без ответа и без общего проекта) — устойчивость связи не выводится"}
	case l.SharedCC > 0:
		return "weaken", []string{"OPEN: только общий получатель в письмах (нет прямого контакта) — устойчивость связи не выводится"}
	default:
		return "weaken", []string{"OPEN: нет прямых свидетельств контакта"}
	}
}

// recipientRole возвращает вес роли получателя в письме (для письма от Q,
// где target — получатель). 0 = письмо не адресовано email напрямую.
func recipientRole(m MsgRow, email string) float64 {
	for _, e := range m.To {
		if strings.EqualFold(e, email) {
			return roleTO
		}
	}
	for _, e := range m.CC {
		if strings.EqualFold(e, email) {
			return roleCC
		}
	}
	for _, e := range m.BCC {
		if strings.EqualFold(e, email) {
			return roleBCC
		}
	}
	return 0
}

// commitDates — даты коммитов (RFC3339).
func commitDates(cs []CommitRow) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Date)
	}
	return out
}

// inPeriod — дата (RFC3339) в периоде фильтра; непарсящаяся дата → true
// (не отсекаем по ошибке формата).
func inPeriod(rfc3339 string, f Filter) bool {
	if f.Since.IsZero() && f.Until.IsZero() {
		return true
	}
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return true
	}
	if !f.Since.IsZero() && t.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && t.After(f.Until) {
		return false
	}
	return true
}

// dayOnly — YYYY-MM-DD из RFC3339 (пусто при ошибке/пустой строке).
func dayOnly(rfc3339 string) string {
	if len(rfc3339) >= 10 {
		return rfc3339[:10]
	}
	return ""
}

// spanJoin расширяет период "a..b" новой датой d (d — YYYY-MM-DD).
func spanJoin(period, d string) string {
	if d == "" {
		return period
	}
	if period == "" {
		return d
	}
	first, last := splitPeriod(period)
	if d < first {
		first = d
	}
	if d > last {
		last = d
	}
	return joinSpan(first, last)
}

// mergePeriods объединяет два периода "a..b" (или одиночные даты) в один.
func mergePeriods(p1, p2 string) string {
	if p1 == "" {
		return p2
	}
	if p2 == "" {
		return p1
	}
	f1, l1 := splitPeriod(p1)
	f2, l2 := splitPeriod(p2)
	first, last := f1, l1
	if f2 < first {
		first = f2
	}
	if l2 > last {
		last = l2
	}
	return joinSpan(first, last)
}

// splitPeriod разбирает "a" или "a..b" на границы (даты YYYY-MM-DD).
func splitPeriod(p string) (first, last string) {
	first, last = p, p
	if i := strings.Index(p, ".."); i > 0 {
		first, last = p[:i], p[i+2:]
	}
	return first, last
}

func joinSpan(first, last string) string {
	if first == "" {
		return ""
	}
	if first == last {
		return first
	}
	return first + ".." + last
}

// spanDays — период "YYYY-MM-DD..YYYY-MM-DD" по списку RFC3339 дат.
func spanDays(dates []string) string {
	if len(dates) == 0 {
		return ""
	}
	first, last := "", ""
	for _, d := range dates {
		if x := dayOnly(d); x != "" {
			if first == "" || x < first {
				first = x
			}
			if last == "" || x > last {
				last = x
			}
		}
	}
	if first == "" {
		return ""
	}
	if first == last {
		return first
	}
	return first + ".." + last
}

// OnlyKind возвращает связи только заданного kind (person|company|service),
// сохраняя порядок. CRM-манифест eslider@ (N-1.5 #275) — только person:
// company (потребительские/торговые) и service в CRM не идут.
func OnlyKind(links []Link, kind string) []Link {
	if kind == "" {
		return links
	}
	out := make([]Link, 0, len(links))
	for _, l := range links {
		if l.Kind == kind {
			out = append(out, l)
		}
	}
	return out
}
