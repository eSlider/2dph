package mailconv

// Юнит-тесты классификатора адреса сети (N-1.1 #268): person | company |
// service. Synthetic (Alice/Bob/example.com) + публичные сервис-домены и
// паттерны реальных accept-списков (gitlab/paypal/linkedin-релеи/трекеры).
// cgo-free. База — IsMachineSender (junkDomains/junkLocalParts); сетевые
// расширения — сервис-домены/подсистемы/рассылки (см. classify.go).

import "testing"

func cls(email, name string) string {
	return ClassifySender(ParsedAddress{Name: name, Email: email})
}

// --- сервис по домену (аккаунт-платформы/рассылки реальных списков #268) ---

func TestClassifyServiceDomains(t *testing.T) {
	cases := []struct{ email, name string }{
		{"gitlab@mg.gitlab.com", "GitLab"},
		{"service@paypal.de", "service@paypal.de"},
		{"info@markets-platform.com", ""},
		{"jobs-noreply@linkedin.com", "LinkedIn"},
		{"notifications-noreply@linkedin.com", "LinkedIn"},
		{"mailrobot@mail.xing.com", "XING"},
		{"magic@djinni.co", "Djinni.co"},
		{"noreply@github.com", "GitHub"},
		{"notifications@github.com", "Andriy Oblivantsev"},
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
	// (#268 acceptance: Ruby Rodriguez, Johanna Eckerstorfer проходят).
	cases := []struct{ email, name string }{
		{"hit-reply@linkedin.com", "Ruby Rodriguez"},
		{"inmail-hit-reply@linkedin.com", "Johanna Eckerstorfer"},
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != KindPerson {
			t.Errorf("ClassifySender(%q, %q) = %q, want person (relay)", c.email, c.name, got)
		}
	}
	// а вот дайджест-релей (не прямой контакт) и сервисные local — service.
	for _, c := range []struct{ email, name string }{
		{"messaging-digest-noreply@linkedin.com", "Sriram Tipirneni via LinkedIn"},
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
		{"noreply@stromnetz-hamburg.de", ""}, // корпоративный домен, но noreply
		{"account-security-noreply@accountprotection.example.com", "Microsoft-Konto-Team"},
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
		{"chiliproject@trac.wheregroup.com", ""}, // трекер ChiliProject (гдеgroup)
		{"projeqtor@wheregroup.com", ""},         // ProjeQtOr
		{"gitlab@wheregroup.com", "GitLab"},      // self-hosted gitlab компании
		{"gitlab@gitlab", "GitLab"},              // внутренний одно-лейбл домен
		{"apache@wiki.wheregroup.com", "MediaWiki Mail"},
		{"dev@trac.example.com", ""},                  // левый лейбл домена = trac
		{"mapbender_dev-request@lists.osgeo.org", ""}, // список рассылки
		{"mapbender_users-owner@lists.example.org", ""},
	}
	for _, c := range cases {
		if got := cls(c.email, c.name); got != KindService {
			t.Errorf("ClassifySender(%q, %q) = %q, want service (subsystem/list)", c.email, c.name, got)
		}
	}
}

// --- компания-контекст: домен компании НЕ сервис (спец-исключение #268) ---

func TestClassifyCompanyContextNotService(t *testing.T) {
	cases := []struct{ email, name, want string }{
		// коллеги гдеgroup — люди
		{"astrid.emde@wheregroup.com", "Astrid Emde", KindPerson},
		{"uli.rothstein@wheregroup.com", "U. Rothstein (WhereGroup)", KindPerson},
		{"a.rashid.pour@wheregroup.com", "Arash Pour", KindPerson},
		// роль/ящик компании — company, а НЕ service
		{"info@wheregroup.com", "WhereGroup", KindCompany},
		{"alle@wheregroup.com", "alle", KindCompany},
		{"wartung@wheregroup.com", "WhereGroup Wartung", KindCompany},
		{"entwicklung@wheregroup.com", "", KindCompany},
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
		{"celine.schlindwein@workwise.io", "Celine Schlindwein von Workwise"}, // workwise.io — НЕ сервис
		{"annabella.krahl@workwise.io", "Annabella Krahl von Workwise"},
		{"dbrondo@caixabank.com", "DANIEL BRONDO KUNKEL"}, // ALLCAPS-имя
		{"friedland.vinylfly@gmail.com", "LAURA FRIEDLAND"},
		{"ralf1.meyer@vattenfall.de", "Ralf Meyer"},
		{"sscp@web.de", "Paul Schmidt"},
		{"programmleiter@projektron.info", "Jens Schaefermeyer"},
		{"dietmobil@web.de", "D. Fleischhauer"},
		{"wizard@oz.net", "Wizard of Oz"}, // частица of
		// без display name: firstname.lastname@домен — человек
		{"astrid.emde@example.com", ""},
		{"arne.schubert@example.com", ""},
		{"m.reuter@example.com", ""}, // инициал.фамилия
		{"apiko@dyvenia.com", "Aranka Piko"},
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
		{"kundenservice@hanseaticbank.de", ""},
		{"bewerbungen@workwise.io", "ScriptRunner Software GmbH Bewerbungen"}, // GmbH
		{"status-update@workwise.io", "Status-Update Workwise"},
		{"team@bit.dev", "Team Bit"},
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
