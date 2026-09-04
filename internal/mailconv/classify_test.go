package mailconv

// Юнит-тесты классификатора адреса сети: person | company | service.
// Полностью синтетика (example.com и вымышленные компании) + публичные
// сервис-домены (gitlab/paypal/linkedin-релеи/трекеры).
// cgo-free. База — IsMachineSender (junkDomains/junkLocalParts); сетевые
// расширения — сервис-домены/подсистемы/рассылки (см. classify.go).

import "testing"

func cls(email, name string) string {
	return ClassifySender(ParsedAddress{Name: name, Email: email})
}

// --- сервис по домену (аккаунт-платформы/рассылки) ---

func TestClassifyServiceDomains(t *testing.T) {
	cases := []struct{ email, name string }{
		{"gitlab@mg.gitlab.com", "GitLab"},
		{"service@paypal.de", "service@paypal.de"},
		{"info@markets.example", ""},
		{"jobs-noreply@linkedin.com", "LinkedIn"},
		{"notifications-noreply@linkedin.com", "LinkedIn"},
		{"mailrobot@mail.xing.com", "XING"},
		{"magic@djinni.co", "Djinni.co"},
		{"noreply@github.com", "GitHub"},
		{"notifications@github.com", "Pat Doe"},
		{"team@hi.wellfound.com", "Wellfound"},
		{"azuredevops@microsoft.com", "Azure DevOps"},
		{"no-reply@accounts.google.com", "Google"},
		{"security@mail.instagram.com", "Instagram"},
		{"notifications@discord.com", "Discord"},
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != KindService {
			t.Errorf("ClassifySender(%q, %q) = %q, want service", c.email, c.name, got)
		}
	}
}

// --- релеи людей через платформы: display name = реальный человек ---

func TestClassifyLinkedInPersonRelay(t *testing.T) {
	// hit-reply / inmail-hit-reply на linkedin.com — InMail-релеи ЛЮДЕЙ
	// (acceptance: Pat Doe, Taylor Jones проходят).
	cases := []struct{ email, name string }{
		{"hit-reply@linkedin.com", "Pat Doe"},
		{"inmail-hit-reply@linkedin.com", "Taylor Jones"},
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != KindPerson {
			t.Errorf("ClassifySender(%q, %q) = %q, want person (relay)", c.email, c.name, got)
		}
	}
	// а вот дайджест-релей (не прямой контакт) и сервисные local — service.
	for _, c := range []struct{ email, name string }{
		{"messaging-digest-noreply@linkedin.com", "Alex Morgan via LinkedIn"},
		{"member@linkedin.com", "LinkedIn"},
		{"linkedin_support@cs.linkedin.com", "LinkedIn Customer Support"},
	} {
		if got := cls(c.email, c.name); got != KindService {
			t.Errorf("ClassifySender(%q, %q) = %q, want service", c.email, c.name, got)
		}
	}
}

// --- локальные части-автоматы (в т.ч. на «человеческих» доменах) ---

func TestClassifyMachineLocalParts(t *testing.T) {
	cases := []struct{ email, name string }{
		{"noreply@example.com", ""},
		{"no-reply@example.com", ""},
		{"donotreply@example.com", ""},
		{"do-not-reply@example.com", ""},
		{"mailer-daemon@example.com", "Mail Delivery System"},
		{"postmaster@example.com", ""},
		{"mailrobot@example.com", ""},
		{"bounces@example.com", ""},
		{"notifications-noreply@example.com", ""},
		{"jobalerts@example.com", "Job Alerts"},
		{"newsletter@example.com", ""},
		{"news@example.com", ""},
		{"marketing@example.com", ""},
		{"subscribe@example.com", ""},
		{"security-noreply@example.com", ""},
		{"noreply@utility.example", ""}, // корпоративный домен, но noreply
		{"account-security-noreply@accountprotection.example.com", "Account Security Team"},
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != KindService {
			t.Errorf("ClassifySender(%q, %q) = %q, want service (machine local)", c.email, c.name, got)
		}
	}
}

// --- подсистемы/трекеры: tool-имя в local или левом лейбле домена ---

func TestClassifySubsystemTrackers(t *testing.T) {
	cases := []struct{ email, name string }{
		{"chiliproject@trac.corp.example", ""}, // трекер ChiliProject
		{"projeqtor@corp.example", ""},         // ProjeQtOr
		{"gitlab@corp.example", "GitLab"},      // self-hosted gitlab компании
		{"gitlab@gitlab", "GitLab"},            // внутренний одно-лейбл домен
		{"apache@wiki.corp.example", "MediaWiki Mail"},
		{"dev@trac.example.com", ""},                  // левый лейбл домена = trac
		{"webdev-request@lists.corp.example", ""},     // список рассылки
		{"webdev_users-owner@lists.example.org", ""},
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != KindService {
			t.Errorf("ClassifySender(%q, %q) = %q, want service (subsystem/list)", c.email, c.name, got)
		}
	}
}

// --- компания-контекст: домен компании НЕ сервис (спец-исключение) ---

func TestClassifyCompanyContextNotService(t *testing.T) {
	cases := []struct{ email, name, want string }{
		// коллеги компании — люди
		{"alex.rivera@example.com", "Alex Rivera", KindPerson},
		{"j.public@example.com", "J. Public (ExampleCorp)", KindPerson},
		{"m.jordan@example.com", "Morgan Jordan", KindPerson},
		// роль/ящик компании — company, а НЕ service
		{"info@example.com", "ExampleCorp", KindCompany},
		{"alle@example.com", "alle", KindCompany},
		{"wartung@example.com", "ExampleCorp Wartung", KindCompany},
		{"entwicklung@example.com", "", KindCompany},
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != c.want {
			t.Errorf("ClassifySender(%q, %q) = %q, want %s", c.email, c.name, got, c.want)
		}
	}
}

// --- люди по display name и по local first.last ---

func TestClassifyPersons(t *testing.T) {
	cases := []struct{ email, name string }{
		{"alex.morgan@partner.example", "Alex Morgan von Acme"}, // домен — не сервис
		{"ann.jones@partner.example", "Ann Jones von Acme"},
		{"d.morgan@corp.example", "DANIEL MORGAN"}, // ALLCAPS-имя
		{"jane.doe@gmail.com", "JANE DOE"},
		{"ralf1.meyer@corp.example", "Ralf Meyer"}, // цифра в local
		{"sscp@mail.example.net", "Paul Schmidt"},
		{"programmleiter@corp.example", "Sam Rivera"},
		{"dienst@mail.example.net", "D. Sample"},
		{"wizard@oz.net", "Wizard of Oz"}, // частица of
		// без display name: firstname.lastname@домен — человек
		{"jane.doe@example.com", ""},
		{"alex.rivera@example.com", ""},
		{"m.public@example.com", ""}, // инициал.фамилия
		{"apiko@corp.example", "Riley Quinn"},
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != KindPerson {
			t.Errorf("ClassifySender(%q, %q) = %q, want person", c.email, c.name, got)
		}
	}
}

// --- компании/роли (не сервис, не человек) ---

func TestClassifyCompanies(t *testing.T) {
	cases := []struct{ email, name string }{
		{"kundenservice@corp.example", ""},
		{"bewerbungen@corp.example", "Acme Software GmbH Bewerbungen"}, // GmbH
		{"status-update@corp.example", "Status-Update Acme"},
		{"team@corp.example", "Team Acme"},
		{"rechnungsstelle@example.com", ""},
		{"info@example.com", ""},
		{"office@example.com", ""},
		{"no-name@example.com", ""}, // один не-человеческий токен
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != KindCompany {
			t.Errorf("ClassifySender(%q, %q) = %q, want company", c.email, c.name, got)
		}
	}
}

// --- границы: пусто/битый email; «robots.txt» в имени не мешает ---

func TestClassifyEdgeCases(t *testing.T) {
	if got := cls("", ""); got != KindService {
		t.Errorf("empty email = %q, want service", got)
	}
	if got := cls("no-at-sign", ""); got != KindService {
		t.Errorf("no @ = %q, want service", got)
	}
	// человек, чей проект про robots.txt, с обычным email — person.
	if got := cls("jane@example.com", "Jane Developer (robots.txt project)"); got != KindPerson {
		t.Errorf("robots.txt in name = %q, want person", got)
	}
	// noreply на домене «людей» — всё равно автомат.
	if got := cls("noreply@example.com", "Alice Example"); got != KindService {
		t.Errorf("noreply with person name = %q, want service", got)
	}
	// регистр и пробелы не влияют.
	if got := cls("  NoReply@Example.COM ", " "); got != KindService {
		t.Errorf("messy noreply = %q, want service", got)
	}
}
