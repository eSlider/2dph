package mailconv

// Raw message.json access for reconcilers: walk the var/corpus/mail tree, decode
// headers, and classify senders (machine vs human).

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
)

// MessagePaths returns every message.json under root (walked recursively).
func MessagePaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "message.json" {
			paths = append(paths, p)
		}
		return nil
	})
	return paths, err
}

// LoadMessages decodes every message.json under root. Unreadable files are
// skipped with a count (a bad file must not kill a sync wave).
func LoadMessages(root string) (msgs []Message, skipped int, err error) {
	paths, err := MessagePaths(root)
	if err != nil {
		return nil, 0, err
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			skipped++
			continue
		}
		var m Message
		if json.Unmarshal(data, &m) != nil {
			skipped++
			continue
		}
		msgs = append(msgs, m)
	}
	return msgs, skipped, nil
}

// ParsedAddress is a decoded RFC5322 address header value.
type ParsedAddress struct {
	Name  string
	Email string
}

// ParseAddress splits `"Name" <a@b>` into its parts (empty when unparseable).
func ParseAddress(raw string) ParsedAddress {
	a, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		if i := strings.LastIndex(raw, "<"); i > 0 && strings.HasSuffix(raw, ">") {
			return ParsedAddress{
				Name:  strings.Trim(strings.TrimSpace(raw[:i]), `"`),
				Email: strings.ToLower(strings.TrimSpace(raw[i+1 : len(raw)-1])),
			}
		}
		return ParsedAddress{Email: strings.ToLower(strings.TrimSpace(raw))}
	}
	return ParsedAddress{Name: strings.TrimSpace(a.Name), Email: strings.ToLower(a.Address)}
}

// junkLocalParts and junkDomains flag machine senders that must never become
// CRM contacts (newsletters, bounces, notifications).
var (
	junkLocalParts = []string{"no-reply", "noreply", "donotreply", "do-not-reply",
		"mailer-daemon", "postmaster", "bounces", "bounce", "notifications",
		"newsletter", "news", "info@", "jobs-noreply", "reply"}
	junkDomains = []string{"linkedin.com", "github.com", "github.io", "google.com",
		"googlemail.com", "facebook.com", "x.com", "twitter.com", "amazon.com",
		"upwork.com", "freelancermap.de", "glassdoor.com", "indeed.com",
		"stepstone.de", "mailchimp.com", "sendgrid.net", "slack.com"}
)

// isJunkDomain reports whether domain (lowercase) is a junk/machine domain or a
// subdomain of one. Shared by IsMachineSender and the network classifier
// (ClassifySender, classify.go) — one implementation for the domain list.
func isJunkDomain(domain string) bool {
	for _, d := range junkDomains {
		if domain == d || strings.HasSuffix(domain, "."+d) {
			return true
		}
	}
	return false
}

// IsMachineSender reports whether the address is a machine/newsletter sender.
func IsMachineSender(a ParsedAddress) bool {
	if a.Email == "" || !strings.Contains(a.Email, "@") {
		return true
	}
	local := strings.ToLower(a.Email[:strings.IndexByte(a.Email, '@')])
	domain := strings.ToLower(a.Email[strings.IndexByte(a.Email, '@')+1:])
	for _, j := range junkLocalParts {
		if strings.Contains(local, strings.TrimSuffix(j, "@")) {
			return true
		}
	}
	return isJunkDomain(domain)
}

// SplitPersonName derives (given, family) from a display name; single-token
// names put everything into given and derive family from the email local part.
// Трейлинговая скобочная декорация « (ExampleCorp)»/« (ExampleOrg e.V.)» (N-1.4
// #271) снимается до разбора — иначе уходит в given/family («Jane Doe
// (ExampleOrg»/«e.V.)», пилот N-1.3 #270).
func SplitPersonName(name, email string) (given, family string) {
	name = stripOrgDecoration(strings.TrimSpace(name))
	if name == "" {
		local := email
		if i := strings.IndexByte(local, '@'); i > 0 {
			local = local[:i]
		}
		parts := strings.FieldsFunc(local, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '.' || r == '-' || r == '_')
		})
		for i, p := range parts {
			parts[i] = strings.Title(strings.ToLower(p)) //nolint:staticcheck // deterministic capitalize
		}
		if len(parts) > 1 {
			return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
		}
		if len(parts) == 1 {
			return parts[0], ""
		}
		return "", ""
	}
	fields := strings.Fields(name)
	switch len(fields) {
	case 0:
		return "", ""
	case 1:
		return fields[0], ""
	default:
		last := fields[len(fields)-1]
		if len(last) == 1 || strings.HasSuffix(last, ".") { // middle initial → keep first two words as given
			return strings.Join(fields[:len(fields)-1], " "), last
		}
		return strings.Join(fields[:len(fields)-1], " "), last
	}
}

// stripOrgDecoration снимает завершающую скобочную декорацию display name:
// «Jane Doe (ExampleOrg e.V.)» → «Jane Doe», «(ExampleCorp)» целиком → «».
// Снимается только сбалансированная пара скобок в КОНЦЕ строки после пробела
// (или занимающая всю строку); повторяется, пока декорации не кончатся.
// «name(org)» без пробела и несбалансированные скобки не трогаются.
func stripOrgDecoration(name string) string {
	for {
		s := strings.TrimSpace(name)
		if !strings.HasSuffix(s, ")") {
			return s
		}
		open, depth := -1, 0
		for i := len(s) - 1; i >= 0; i-- {
			switch s[i] {
			case ')':
				depth++
			case '(':
				depth--
				if depth == 0 {
					open = i
				}
			}
			if open >= 0 {
				break
			}
		}
		if open < 0 || (open > 0 && s[open-1] != ' ') {
			return s
		}
		name = s[:open]
	}
}

// FormatNote renders one deterministic interaction line for CRM history.
func FormatNote(m Message) string {
	from := ParseAddress(m.From)
	when := m.ReceivedAt
	if when.IsZero() {
		when = m.Date
	}
	subject := strings.TrimSpace(m.Subject)
	if subject == "" {
		subject = "(no subject)"
	}
	return fmt.Sprintf("[email:%s/%s] %s — from %s <%s> · %s",
		m.Source, m.ID, subject, from.Name, from.Email,
		when.Format("2006-01-02 15:04"))
}
