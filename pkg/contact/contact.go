package contact

import "strings"

// Contact is one normalized address-book entry from any source.
type Contact struct {
	FullName string
	Given    string
	Family   string
	Emails   []string
	Phones   []string
	Org      string
	Title    string
	Photo    string
	Source   string
}

// HasIdentity reports whether the contact carries a usable name or any
// reachable address. Empty rows (all blank) are dropped.
func (c Contact) HasIdentity() bool {
	return strings.TrimSpace(c.FullName) != "" ||
		strings.TrimSpace(c.Given) != "" ||
		strings.TrimSpace(c.Family) != "" ||
		len(c.Emails) > 0 ||
		len(c.Phones) > 0 ||
		strings.TrimSpace(c.Org) != ""
}

// DisplayName returns the best available human name for a contact.
func (c Contact) DisplayName() string {
	if s := strings.TrimSpace(c.FullName); s != "" {
		return s
	}
	g := strings.TrimSpace(c.Given)
	f := strings.TrimSpace(c.Family)
	switch {
	case g != "" && f != "":
		return strings.TrimSpace(g + " " + f)
	case g != "":
		return g
	case f != "":
		return f
	case len(c.Emails) > 0:
		return c.Emails[0]
	case len(c.Phones) > 0:
		return c.Phones[0]
	}
	return "(unnamed)"
}

// Markdown renders the contact as a compact leaf snippet for the brain.
func (c Contact) Markdown() string {
	var b strings.Builder
	b.WriteString("Contact: " + c.DisplayName())
	if c.Title != "" {
		b.WriteString(" — " + c.Title)
	}
	if c.Org != "" {
		b.WriteString(" @ " + c.Org)
	}
	b.WriteString("\n")
	if len(c.Emails) > 0 {
		b.WriteString("Emails: " + strings.Join(c.Emails, ", ") + "\n")
	}
	if len(c.Phones) > 0 {
		b.WriteString("Phones: " + strings.Join(c.Phones, ", ") + "\n")
	}
	return strings.TrimSpace(b.String())
}
