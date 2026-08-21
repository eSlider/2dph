package contact

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
)

// Dedupe removes duplicate contacts by (display name, first email).
func Dedupe(cs []Contact) []Contact {
	seen := map[string]bool{}
	var out []Contact
	for _, c := range cs {
		key := strings.ToLower(strings.TrimSpace(c.DisplayName()))
		if len(c.Emails) > 0 {
			key += "|" + strings.ToLower(c.Emails[0])
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// Render serializes contacts in the given format: json, csv, or leaf.
func Render(cs []Contact, format string) (string, error) {
	switch format {
	case "json":
		b, err := json.MarshalIndent(cs, "", "  ")
		return string(b), err
	case "csv":
		var b strings.Builder
		w := csv.NewWriter(&b)
		_ = w.Write([]string{"full_name", "given", "family", "emails", "phones", "org", "title", "photo", "source"})
		for _, c := range cs {
			_ = w.Write([]string{
				c.FullName, c.Given, c.Family,
				strings.Join(c.Emails, ";"), strings.Join(c.Phones, ";"),
				c.Org, c.Title, c.Photo, c.Source,
			})
		}
		w.Flush()
		return b.String(), w.Error()
	case "leaf":
		var b strings.Builder
		for _, c := range cs {
			b.WriteString(c.Markdown())
			b.WriteString("\n\n")
		}
		return b.String(), nil
	default:
		return "", fmt.Errorf("unknown format %q (want json|csv|leaf)", format)
	}
}

// PrintCounts writes a human summary of the loaded contacts to stdout.
func PrintCounts(cs []Contact) {
	emails := 0
	phones := 0
	bySource := map[string]int{}
	for _, c := range cs {
		emails += len(c.Emails)
		phones += len(c.Phones)
		bySource[c.Source]++
	}
	fmt.Printf("contacts: %d total (%d emails, %d phones)\n", len(cs), emails, phones)
	for src, n := range bySource {
		fmt.Printf("  %4d  %s\n", n, src)
	}
}
