package chats

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	cliparse "github.com/eSlider/2dph/pkg/cli"
)

var (
	phoneRegex    = regexp.MustCompile(`[+\d][\d\s\-()]{6,25}\d`)
	dateRegex     = regexp.MustCompile(`^\d{2,4}[-/]\d{1,2}[-/]\d{2,4}$`)
	rangeRegex    = regexp.MustCompile(`^\d+\s*[-–]\s*\d+$`)
	linkedinRegex = regexp.MustCompile(`linkedin\.com/in/[\w-]+`)
	emailRegex    = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	projectRegex  = regexp.MustCompile(`(?i)project\s*[:/]\s*(.+)`)
	dealRegex     = regexp.MustCompile(`(?i)(deal|opportunity)\s*[:/]\s*(.+)`)
	skillRegex    = regexp.MustCompile(`(?i)(works?|worked|working)\s+(at|on|with)\s+([A-Z][\w\s]+)`)
)

func isValidPhone(s string) bool {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "+()-\t ")
	if len(s) < 6 || len(s) > 25 {
		return false
	}
	if dateRegex.MatchString(s) || rangeRegex.MatchString(s) {
		return false
	}
	if strings.ContainsAny(s, "/abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return false
	}
	if strings.Contains(s, "000") || strings.Contains(s, "500 ") || strings.Contains(s, "000 ") {
		return false
	}
	digits := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if digits < 7 || digits > 15 {
		return false
	}
	// Card number pattern: 16 digits with possible spaces
	if digits == 16 {
		return false
	}
	// Date-like: 8 digits starting with 20xx or 19xx
	if len(s) <= 8 && digits == 8 && (strings.HasPrefix(s, "20") || strings.HasPrefix(s, "19")) {
		return false
	}
	// 11+ digits starting with 2 - unlikely phone
	if digits >= 11 && strings.HasPrefix(s, "2") && !strings.HasPrefix(s, "+") {
		return false
	}
	// Must start with + or be at least 7 digits
	if !strings.HasPrefix(s, "+") && digits < 7 {
		return false
	}
	return true
}

type ExtractedFact struct {
	ChatID    string `json:"chat_id"`
	ChatName  string `json:"chat_name"`
	Platform  string `json:"platform"`
	FactType  string `json:"fact_type"`
	Value     string `json:"value"`
	Source    string `json:"source"`
	MessageID string `json:"message_id"`
}

func RunFacts(args []string) int {
	if err := parseNoFlags("chats-facts", args); err != nil {
		return cliparse.Fail(err)
	}

	root := Dir()
	telegramDir := filepath.Join(root, "telegram")

	entries, err := os.ReadDir(telegramDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chats facts: read %s: %v\n", telegramDir, err)
		return 1
	}

	var allFacts []ExtractedFact
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		chatID := entry.Name()
		jsonlPath := filepath.Join(telegramDir, chatID, "messages.jsonl")
		info, err := os.Stat(jsonlPath)
		if err != nil {
			continue
		}
		if info.Size() == 0 {
			continue
		}

		facts, chatName := extractFacts(jsonlPath, chatID)
		allFacts = append(allFacts, facts...)
		_ = chatName
	}

	if len(allFacts) == 0 {
		fmt.Println("chats facts: no facts extracted")
		return 0
	}

	phoneFacts := filterFacts(allFacts, "phone")
	emailFacts := filterFacts(allFacts, "email")
	linkedinFacts := filterFacts(allFacts, "linkedin")
	projectFacts := filterFacts(allFacts, "project")
	skillFacts := filterFacts(allFacts, "skill")

	fmt.Printf("chats facts: extracted %d facts (%d phone, %d email, %d linkedin, %d project, %d skill)\n",
		len(allFacts), len(phoneFacts), len(emailFacts), len(linkedinFacts), len(projectFacts), len(skillFacts))

	factsDir := filepath.Join(root, "facts")
	if err := os.MkdirAll(factsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "chats facts: mkdir %s: %v\n", factsDir, err)
		return 1
	}
	factsPath := filepath.Join(factsDir, "chat-facts.json")
	data, err := json.MarshalIndent(allFacts, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "chats facts: marshal: %v\n", err)
		return 1
	}
	if err := os.WriteFile(factsPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "chats facts: write %s: %v\n", factsPath, err)
		return 1
	}
	fmt.Printf("chats facts: saved to %s\n", factsPath)

	writeFactsMarkdown(allFacts)

	return 0
}

func extractFacts(jsonlPath, chatID string) ([]ExtractedFact, string) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, ""
	}
	defer f.Close()

	var facts []ExtractedFact
	chatName := ""

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if chatName == "" && msg.From != "" {
			chatName = msg.From
		}

		text := msg.Text

		phones := phoneRegex.FindAllString(text, -1)
		for _, p := range phones {
			p = strings.TrimSpace(p)
			p = strings.Trim(p, "()- \t")
			if isValidPhone(p) {
				facts = append(facts, ExtractedFact{
					ChatID:    chatID,
					ChatName:  chatName,
					Platform:  "telegram",
					FactType:  "phone",
					Value:     p,
					Source:    "chat:" + msg.ID,
					MessageID: msg.ID,
				})
			}
		}

		emails := emailRegex.FindAllString(text, -1)
		for _, e := range emails {
			facts = append(facts, ExtractedFact{
				ChatID:    chatID,
				ChatName:  chatName,
				Platform:  "telegram",
				FactType:  "email",
				Value:     strings.ToLower(e),
				Source:    "chat:" + msg.ID,
				MessageID: msg.ID,
			})
		}

		linkedins := linkedinRegex.FindAllString(text, -1)
		for _, l := range linkedins {
			facts = append(facts, ExtractedFact{
				ChatID:    chatID,
				ChatName:  chatName,
				Platform:  "telegram",
				FactType:  "linkedin",
				Value:     "https://" + l,
				Source:    "chat:" + msg.ID,
				MessageID: msg.ID,
			})
		}

		if matches := projectRegex.FindStringSubmatch(text); len(matches) > 1 {
			facts = append(facts, ExtractedFact{
				ChatID:    chatID,
				ChatName:  chatName,
				Platform:  "telegram",
				FactType:  "project",
				Value:     strings.TrimSpace(matches[1]),
				Source:    "chat:" + msg.ID,
				MessageID: msg.ID,
			})
		}

		if matches := dealRegex.FindStringSubmatch(text); len(matches) > 2 {
			facts = append(facts, ExtractedFact{
				ChatID:    chatID,
				ChatName:  chatName,
				Platform:  "telegram",
				FactType:  "deal",
				Value:     strings.TrimSpace(matches[2]),
				Source:    "chat:" + msg.ID,
				MessageID: msg.ID,
			})
		}

		if matches := skillRegex.FindStringSubmatch(text); len(matches) > 3 {
			facts = append(facts, ExtractedFact{
				ChatID:    chatID,
				ChatName:  chatName,
				Platform:  "telegram",
				FactType:  "skill",
				Value:     strings.TrimSpace(matches[0]),
				Source:    "chat:" + msg.ID,
				MessageID: msg.ID,
			})
		}
	}
	return facts, chatName
}

func filterFacts(facts []ExtractedFact, factType string) []ExtractedFact {
	var result []ExtractedFact
	for _, f := range facts {
		if f.FactType == factType {
			result = append(result, f)
		}
	}
	return result
}

// writeFactsMarkdown stores a sidecar for humans. Brain ingest is
// bin/brain/index.go (not this subject).
func writeFactsMarkdown(facts []ExtractedFact) {
	mdDir := filepath.Join(Dir(), "facts")
	if err := os.MkdirAll(mdDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "chats facts: mkdir %s: %v\n", mdDir, err)
		return
	}

	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("root: info\n")
	sb.WriteString("---\n\n")
	sb.WriteString("# Chat-Derived Facts\n\n")
	for _, f := range facts {
		sb.WriteString(fmt.Sprintf("- **%s**: %s (source: %s, chat: %s)\n",
			f.FactType, f.Value, f.Source, f.ChatName))
	}
	sb.WriteString("\n")

	factsMD := filepath.Join(mdDir, "chat-facts.md")
	if err := os.WriteFile(factsMD, []byte(sb.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "chats facts: write %s: %v\n", factsMD, err)
		return
	}
	fmt.Printf("chats facts: markdown %s (index via brain, not chats)\n", factsMD)
}
