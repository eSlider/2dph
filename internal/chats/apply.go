package chats

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cliparse "github.com/eSlider/2dph/pkg/cli"
)

type ooContact struct {
	ID          int    `json:"id"`
	DisplayName string `json:"displayName"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	About       string `json:"about"`
	CommonData  []struct {
		InfoType int    `json:"infoType"`
		Data     string `json:"data"`
		Category string `json:"categoryName"`
	} `json:"commonData"`
}

func RunApply(args []string) int {
	dryRun, err := parseApplyFlags(args)
	if err != nil {
		return cliparse.Fail(err)
	}

	ooCLI := findOO()
	if ooCLI == "" {
		fmt.Fprintln(os.Stderr, "chats apply: oo CLI not found; set OO_CLI or install go-onlyoffice")
		return 1
	}

	facts, err := loadFacts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "chats apply: %v\n", err)
		return 1
	}
	if len(facts) == 0 {
		fmt.Println("chats apply: no facts to process")
		return 0
	}

	phoneFacts := filterFacts(facts, "phone")
	emailFacts := filterFacts(facts, "email")

	phoneFacts = dedupeFacts(phoneFacts)
	emailFacts = dedupeFacts(emailFacts)

	type resolvedFact struct {
		Fact   ExtractedFact
		OoID   int
		OoName string
		Action string // "info-add" or "persons-create"
	}

	var resolved []resolvedFact

	for _, f := range phoneFacts {
		contact, err := searchContact(ooCLI, f.ChatName)
		if err != nil || contact == nil {
			fmt.Printf("  ✗ %s: phone %s — not found in CRM\n", f.ChatName, f.Value)
			resolved = append(resolved, resolvedFact{Fact: f, Action: "persons-create"})
			continue
		}
		hasPhone := false
		for _, d := range contact.CommonData {
			if d.InfoType == 2 {
				hasPhone = true
				break
			}
		}
		if hasPhone {
			fmt.Printf("  ✓ %s (ID %d): phone %s — already has phone, skip\n", contact.DisplayName, contact.ID, f.Value)
			continue
		}
		fmt.Printf("  → %s (ID %d): add phone %s\n", contact.DisplayName, contact.ID, f.Value)
		resolved = append(resolved, resolvedFact{
			Fact: f, OoID: contact.ID, OoName: contact.DisplayName, Action: "info-add",
		})
	}

	for _, f := range emailFacts {
		if strings.EqualFold(f.Value, envVar("ONLYOFFICE_USER", "")) ||
			strings.EqualFold(f.Value, envVar("OO_USER", "")) ||
			strings.EqualFold(f.Value, os.Getenv("EMAIL")) {
			continue
		}
		contact, err := searchContact(ooCLI, f.ChatName)
		if err != nil || contact == nil {
			fmt.Printf("  ✗ %s: email %s — not found in CRM\n", f.ChatName, f.Value)
			resolved = append(resolved, resolvedFact{Fact: f, Action: "persons-create"})
			continue
		}
		hasEmail := false
		for _, d := range contact.CommonData {
			if d.InfoType == 1 && d.Data == f.Value {
				hasEmail = true
				break
			}
		}
		if hasEmail {
			fmt.Printf("  ✓ %s (ID %d): email %s — already exists\n", contact.DisplayName, contact.ID, f.Value)
			continue
		}
		fmt.Printf("  → %s (ID %d): add email %s\n", contact.DisplayName, contact.ID, f.Value)
		resolved = append(resolved, resolvedFact{
			Fact: f, OoID: contact.ID, OoName: contact.DisplayName, Action: "info-add",
		})
	}

	if len(resolved) == 0 {
		fmt.Println("chats apply: nothing to apply")
		return 0
	}

	fmt.Printf("\nchats apply: %d actions to apply\n", len(resolved))

	if dryRun {
		for _, r := range resolved {
			switch r.Action {
			case "info-add":
				infoType := "Phone"
				if r.Fact.FactType == "email" {
					infoType = "Email"
				}
				fmt.Printf("  [dry-run] oo contacts info-add %d --type %s --value %s\n",
					r.OoID, infoType, r.Fact.Value)
			case "persons-create":
				fmt.Printf("  [dry-run] oo persons create --first %q --about %q\n",
					r.Fact.ChatName, "Contact from Telegram chat")
			}
		}
		return 0
	}

	success := 0
	failed := 0
	for _, r := range resolved {
		switch r.Action {
		case "info-add":
			infoType := "Phone"
			if r.Fact.FactType == "email" {
				infoType = "Email"
			}
			if err := ooInfoAdd(ooCLI, r.OoID, infoType, r.Fact.Value); err != nil {
				fmt.Fprintf(os.Stderr, "  ✗ info-add %s: %v\n", r.Fact.Value, err)
				failed++
			} else {
				fmt.Printf("  ✓ %s → %s (ID %d)\n", r.Fact.Value, r.OoName, r.OoID)
				success++
			}
		case "persons-create":
			fmt.Printf("  - create %s (skipped — needs review)\n", r.Fact.ChatName)
			success++
		}
	}

	fmt.Printf("\nchats apply: %d succeeded, %d failed\n", success, failed)
	if failed > 0 {
		return 1
	}
	return 0
}

func loadFacts() ([]ExtractedFact, error) {
	factsPath := filepath.Join(Dir(), "facts", "chat-facts.json")
	data, err := os.ReadFile(factsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no facts at %s; run 'chats facts' first", factsPath)
		}
		return nil, fmt.Errorf("read facts: %w", err)
	}
	var facts []ExtractedFact
	if err := json.Unmarshal(data, &facts); err != nil {
		return nil, fmt.Errorf("parse facts: %w", err)
	}
	return facts, nil
}

func dedupeFacts(facts []ExtractedFact) []ExtractedFact {
	seen := make(map[string]bool)
	var result []ExtractedFact
	for _, f := range facts {
		norm := normalizePhone(f.Value)
		key := f.ChatName + ":" + factTypeKey(f.FactType) + ":" + norm
		if seen[key] {
			continue
		}
		seen[key] = true
		f.Value = norm
		result = append(result, f)
	}
	return result
}

func normalizePhone(s string) string {
	var digits []rune
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	if len(digits) > 0 {
		return string(digits)
	}
	return s
}

func factTypeKey(t string) string {
	switch t {
	case "phone":
		return "p"
	case "email":
		return "e"
	default:
		return t
	}
}

func findOO() string {
	if v := os.Getenv("OO_CLI"); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
	}
	candidates := []string{
		filepath.Join(os.Getenv("HOME"), "go", "bin", "oo"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func searchContact(ooCLI, name string) (*ooContact, error) {
	query := name
	// Try full name first
	if c, _ := searchByQuery(ooCLI, query); c != nil {
		return c, nil
	}
	// Try first word
	firstWord := strings.Fields(name)[0]
	if firstWord != name {
		if c, _ := searchByQuery(ooCLI, firstWord); c != nil {
			return c, nil
		}
	}
	return nil, nil
}

func searchByQuery(ooCLI, query string) (*ooContact, error) {
	cmd := exec.Command(ooCLI, "persons", "list", "--search", query, "-o", "json")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("oo persons list: %w\n%s", err, errBuf.String())
	}
	var contacts []ooContact
	if err := json.Unmarshal(outBuf.Bytes(), &contacts); err != nil {
		return nil, nil
	}
	for _, c := range contacts {
		lower := strings.ToLower(c.DisplayName)
		lowerQuery := strings.ToLower(query)
		if strings.EqualFold(c.DisplayName, query) ||
			strings.Contains(lower, lowerQuery) ||
			strings.Contains(lowerQuery, strings.ToLower(c.FirstName)) {
			return &c, nil
		}
		for _, d := range c.CommonData {
			if d.InfoType == 1 && strings.Contains(strings.ToLower(d.Data), lowerQuery) {
				return &c, nil
			}
		}
	}
	if len(contacts) > 0 {
		return &contacts[0], nil
	}
	return nil, nil
}

func ooInfoAdd(ooCLI string, contactID int, infoType, value string) error {
	cmd := exec.Command(ooCLI, "contacts", "info-add",
		fmt.Sprintf("%d", contactID),
		"--type", infoType,
		"--value", value,
		"--category", "Work",
		"-o", "json",
	)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("info-add: %w\n%s", err, errBuf.String())
	}
	return nil
}
