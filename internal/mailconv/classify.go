package mailconv

// Классификатор адреса сети связей (N-1.1 #268): person | company | service.
// Строится поверх sender-эвристик IsMachineSender (junkDomains/
// junkLocalParts, messages.go) и расширяет их под сеть (L-9.5 #234): из
// accept-списков в CRM не должны попадать сервис-аккаунты/подсистемы/
// рассылки (GitLab/PayPal/LinkedIn/markets-platform/chiliproject@trac и
// аналоги), а реальные люди и компании-контекст — проходят.
//
// Классы (детерминированные, документируются в docs/brain/crm-network-filter.md):
//
//	service — автомат/сервис-домен/подсистема/трекер/рассылка: в CRM не идёт;
//	company — отправитель-организация или роль/ящик (info@, kundenservice@…);
//	person  — человек (личный адрес или display name = реальное имя).
//
// Каскад правил (первое совпадение побеждает):
//
//  1. пустой email / без '@' → service;
//  2. platform-релеи ЛЮДЕЙ (LinkedIn InMail: hit-reply/inmail-hit-reply на
//     linkedin.com) → person: display name = реальный человек, письма идут
//     через релей платформы (Jordan Smith и т.п.);
//  3. junk-домен mailconv (isJunkDomain) или сетевой маркер (сервис-домен,
//     локальная часть-автомат, tool-имя подсистемы/трекера, левый лейбл
//     домена ∈ tool-имена, *-request/*-owner списки) → service;
//  4. display name похож на имя человека → person;
//  5. локальная часть вида firstname.lastname@домен → person;
//  6. иначе → company (роль/организация; консервативно: НЕ исключаем).
//
// Локальные junk-ПОДСТРОКИ mailconv («info@», «reply», «news») в сети
// НЕ используются намеренно: info@компания — роль-ящик (company, спец-
// исключение #268 «домен компании-контекста ≠ сервис»), reply/hit-reply —
// релеи людей через платформы. Вместо них — суффиксы/токены machineLocal*
// (noreply-формы, mailer-daemon, robot, рассылки, security).

import (
	"strings"
	"unicode"
)

// Классы адреса (строки — стабильны в --json/YAML экспорте сети).
const (
	KindService = "service"
	KindCompany = "company"
	KindPerson  = "person"
)

// networkServiceDomains — сервис-домены сети: аккаунт-платформы и
// инфраструктура-отправители из accept-списков (напр. gitlab.com, paypal.com);
// Торговые компании (amazon/ebay/банки/телекомы) сюда НЕ входят — это
// company-класс. Домен компании-контекста отсутствует намеренно:
// компания-контекст — не сервис.
var networkServiceDomains = []string{
	"gitlab.com", "paypal.com", "paypal.de", "markets.example",
	"xing.com", "djinni.co", "wellfound.com", "workablemail.com",
	"coinbase.com", "dynadot.com", "hetzner.com", "netlify.com",
	"letsencrypt.org", "steampowered.com", "instagram.com", "discord.com",
	"microsoft.com", "apple.com",
}

// machineLocalSuffixes — нормализованные (без ._-) СУФФИКСЫ локальной части,
// однозначно автомат: noreply, mailer-daemon, robot, рассылки, security.
// ВАЖНО: "reply" сюда НЕ входит — hit-reply@linkedin.com релеит людей (шаг 2);
// plain-"reply" и так ловит IsMachineSender (junkLocalParts, только вне
// linkedin-релеев — см. порядок каскада).
var machineLocalSuffixes = []string{
	"noreply", "donotreply", "mailerdaemon", "postmaster", "mailrobot",
	"mailer", "robot", "bounce", "bounces", "newsletter", "notifications",
	"jobalerts", "jobalert", "security", "digest", "alerts",
}

// machineLocalTokens — целые токены локальной части = рассылка/новости/автомат.
var machineLocalTokens = map[string]struct{}{
	"news": {}, "marketing": {}, "subscribe": {},
}

// toolNames — имена подсистем/трекеров: встречается в локальной части
// (gitlab@corp.example, projeqtor@…) или как левый лейбл домена
// (trac.corp.example, wiki.corp.example, gitlab) = сервис.
var toolNames = map[string]struct{}{
	"gitlab": {}, "projeqtor": {}, "chiliproject": {}, "trac": {},
	"redmine": {}, "jira": {}, "confluence": {}, "wiki": {}, "mediawiki": {},
	"apache": {}, "svn": {}, "jenkins": {}, "mailman": {}, "sympa": {},
	"nagios": {}, "zabbix": {},
}

// personRelayLocal — локальные части платформенных релеев, через которые
// пишут ЛЮДИ (LinkedIn InMail): display name — реальный человек, не сервис.
// Домен-скоуп: linkedin.com (шаг 2 каскада).
var personRelayLocal = map[string]struct{}{
	"hit-reply": {}, "inmail-hit-reply": {},
}

// orgMarkers — корпоративные суффиксы в display name (регистр/точка любые):
// отправитель — организация, а не человек.
var orgMarkers = []string{
	"gmbh", "ag", "se", "ug", "ltd", "inc", "corp", "kg", "llc", "e.v",
}

// roleWords — служебные/ролевые слова display name (первые или последние):
// «Acme Wartung», «Team Bit», «Status-Update Acme» —
// команды/роли, а не имена людей.
var roleWords = map[string]struct{}{
	"team": {}, "wartung": {}, "neuigkeiten": {}, "bewerbungen": {},
	"rechnung": {}, "kundenservice": {}, "digest": {}, "alerts": {},
	"jobs": {}, "news": {}, "premium": {}, "status": {}, "updates": {},
	"notifications": {}, "security": {}, "delivery": {}, "daemon": {},
	"billing": {}, "receipts": {}, "system": {}, "mail": {}, "support": {},
	"service": {}, "info": {}, "sales": {}, "marketing": {},
}

// particles — частицы, допускаемые в именах (регистронезависимо).
var particles = map[string]struct{}{
	"van": {}, "von": {}, "der": {}, "den": {}, "de": {}, "da": {},
	"del": {}, "di": {}, "la": {}, "of": {}, "zu": {}, "zur": {},
}

// ClassifySender классифицирует адрес (email + display name) сети связей:
// person | company | service. Детерминированная, cgo-free; расширяет
// IsMachineSender, не дублируя его (junk-списки переиспользуются).
func ClassifySender(a ParsedAddress) string {
	email := strings.ToLower(strings.TrimSpace(a.Email))
	name := strings.TrimSpace(a.Name)
	if email == "" || !strings.Contains(email, "@") {
		return KindService
	}
	local := email[:strings.IndexByte(email, '@')]
	domain := email[strings.IndexByte(email, '@')+1:]

	// 1) платформенные релеи ЛЮДЕЙ — до junk-доменов (linkedin.com в junk).
	if domain == "linkedin.com" {
		if _, ok := personRelayLocal[local]; ok {
			return KindPerson
		}
	}
	// 2) автоматы/сервисы: junk-домены mailconv (переиспользуется список и
	// реализация isJunkDomain) + сетевые расширения.
	if isJunkDomain(domain) || isNetworkService(local, domain) {
		return KindService
	}
	// 3) иначе — человек или компания/роль (по display name, затем local).
	if isPersonName(name) {
		return KindPerson
	}
	if isPersonLocal(local) {
		return KindPerson
	}
	return KindCompany
}

// isNetworkService — сетевые расширения поверх IsMachineSender:
// сервис-домены, local-части-автоматы, tool-имена, списки рассылки.
func isNetworkService(local, domain string) bool {
	if matchDomain(domain, networkServiceDomains) {
		return true
	}
	norm := stripSeparators(local)
	for _, s := range machineLocalSuffixes {
		if strings.HasSuffix(norm, s) {
			return true
		}
	}
	for tok := range machineLocalTokens {
		if hasToken(local, tok) {
			return true
		}
	}
	// подсистема/трекер: tool-имя в local или левом лейбле домена.
	if hasAnyToken(local, toolNames) {
		return true
	}
	if first, _, _ := strings.Cut(domain, "."); first != "" {
		if _, ok := toolNames[first]; ok {
			return true
		}
		if first == "lists" {
			return true
		}
	}
	// менеджеры списков рассылки (*-request / *-owner).
	if strings.HasSuffix(local, "-request") || strings.HasSuffix(local, "-owner") {
		return true
	}
	return false
}

// matchDomain — domain равен d или поддомен .d (список доменов).
func matchDomain(domain string, list []string) bool {
	for _, d := range list {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

// stripSeparators удаляет ._- из локальной части (no-reply → noreply).
func stripSeparators(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' {
			return -1
		}
		return r
	}, s)
}

// hasToken — токен token встречается в s (разделители — не буквы/цифры).
func hasToken(s, token string) bool {
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if tok == token {
			return true
		}
	}
	return false
}

func hasAnyToken(s string, set map[string]struct{}) bool {
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if _, ok := set[tok]; ok {
			return true
		}
	}
	return false
}

// splitEmailLocal разбивает локальную часть на сегменты по '.' (first.last).
func splitEmailLocal(local string) []string {
	return strings.Split(local, ".")
}

// isPersonLocal — локальная часть вида firstname.lastname@домен (или
// инициал.фамилия): личный адрес, если display name пуст/не информативен.
func isPersonLocal(local string) bool {
	segs := splitEmailLocal(local)
	if len(segs) < 2 {
		return false
	}
	for i, s := range segs {
		if s == "" {
			return false
		}
		letters := 0
		for _, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if i == 0 {
			// первый сегмент — имя или инициал (1 буква допускается)
			if letters < 1 {
				return false
			}
			continue
		}
		if letters < 2 {
			return false // фамилия не может быть одной буквой
		}
	}
	return true
}

// isPersonName — display name похож на имя человека. Декору убираются:
// « (Организация)», « via X», « von X»; затем проверяется форма токенов
// (Title/ALLCAPS/инициалы/частицы) и отсутствие org/role-маркеров.
func isPersonName(name string) bool {
	if name == "" {
		return false
	}
	core := stripNameDecor(name)
	tokens := strings.Fields(core)
	if len(tokens) < 2 {
		return false
	}
	for _, tok := range tokens {
		lower := strings.ToLower(tok)
		if _, ok := particles[lower]; ok {
			continue
		}
		if isOrgMarker(lower) || isRoleWord(lower) {
			return false
		}
		if !isNameToken(tok) {
			return false
		}
	}
	// последний токен — не роль («Acme Wartung», «Status-Update Acme»).
	last := strings.ToLower(tokens[len(tokens)-1])
	if isRoleWord(last) {
		return false
	}
	return true
}

// stripNameDecor обрезает декорации display name: «Имя (Орг)» и «Имя via X».
// Частицы внутри имени (von/de/of и т.п.) НЕ режутся — они допускаются
// particles (см. isPersonName), иначе «Wizard of Oz»/«Alex … von Acme»
// теряют класс person.
func stripNameDecor(name string) string {
	for _, marker := range []string{" (", " via "} {
		if i := strings.Index(name, marker); i > 0 {
			name = name[:i]
		}
	}
	return strings.TrimSpace(name)
}

// isNameToken — токен display name в форме имени: инициал («U.», «J»),
// Title-case слово, ALLCAPS/акроним; допустимы дефис/апостроф.
func isNameToken(tok string) bool {
	runes := []rune(tok)
	if len(runes) == 0 {
		return false
	}
	// «U.» / «J.» — инициалы (точка снимается).
	if runes[len(runes)-1] == '.' {
		runes = runes[:len(runes)-1]
		if len(runes) == 1 && unicode.IsLetter(runes[0]) {
			return true
		}
	}
	if len(runes) < 2 {
		return false
	}
	first, rest := runes[0], runes[1:]
	if !unicode.IsUpper(first) {
		return false
	}
	hasLower := false
	for _, r := range rest {
		switch {
		case unicode.IsLetter(r):
			if unicode.IsLower(r) {
				hasLower = true
			}
		case r == '-', r == '\'':
		default:
			return false
		}
	}
	if hasLower {
		return true // Title-case («Rivera», «Hernández»)
	}
	// без lowercase: акроним/ALLCAPS (остальные буквы — верхний регистр)
	for _, r := range rest {
		if unicode.IsLetter(r) && !unicode.IsUpper(r) {
			return false
		}
	}
	return true
}

func isOrgMarker(lowerTok string) bool {
	lowerTok = strings.TrimSuffix(lowerTok, ".")
	for _, m := range orgMarkers {
		if lowerTok == m {
			return true
		}
	}
	return false
}

// isRoleWord — токен (или его часть через -/_) — служебное/ролевое слово:
// «status-update», «Acme Wartung», «Status-Update Acme» → не имя.
func isRoleWord(lowerTok string) bool {
	if _, ok := roleWords[lowerTok]; ok {
		return true
	}
	for _, part := range strings.FieldsFunc(lowerTok, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	}) {
		if _, ok := roleWords[part]; ok {
			return true
		}
	}
	return false
}
