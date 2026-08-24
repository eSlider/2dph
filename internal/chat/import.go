package chat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cliparse "github.com/eSlider/2dph/pkg/cli"
)

func RunImport(args []string) int {
	if err := parseNoFlags("chats-import", args); err != nil {
		return cliparse.Fail(err)
	}

	root := Dir()
	mdRoot := filepath.Join(root, "md")
	platforms := []string{"telegram", "whatsapp"}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		p := args[0]
		if !validPlatform(p) {
			fmt.Fprintf(os.Stderr, "chats import: unknown platform %q (want telegram|whatsapp)\n", p)
			return 2
		}
		platforms = []string{p}
	}

	var matches []string
	for _, plat := range platforms {
		glob := filepath.Join(root, plat, "*", "messages.jsonl")
		ms, err := filepath.Glob(glob)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chats import: glob %s: %v\n", glob, err)
			return 1
		}
		matches = append(matches, ms...)
	}
	if len(matches) == 0 {
		fmt.Fprintf(os.Stderr, "chats import: no messages.jsonl found under %s\n", root)
		return 1
	}

	written := 0
	failed := 0
	for _, jsonlPath := range matches {
		platform := platformOf(jsonlPath, root)
		if platform == "" {
			fmt.Fprintf(os.Stderr, "chats import: unhandled platform path %s\n", jsonlPath)
			failed++
			continue
		}
		chatID := filepath.Base(filepath.Dir(jsonlPath))

		messages, chatName, err := readJSONL(jsonlPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "chats import: read %s: %v\n", jsonlPath, err)
			failed++
			continue
		}
		if len(messages) == 0 {
			continue
		}
		if chatName == "" {
			chatName = chatID
		}

		participants := collectParticipants(messages)
		chatType := "personal"
		if len(participants) > 3 {
			chatType = "group"
		}

		firstID := ""
		if len(messages) > 0 {
			firstID = messages[0].ID
		}

		var b bytes.Buffer
		b.WriteString("---\n")
		fmt.Fprintf(&b, "id: %s\n", firstID)
		fmt.Fprintf(&b, "platform: %s\n", platform)
		fmt.Fprintf(&b, "chat_id: %s\n", chatID)
		fmt.Fprintf(&b, "chat_name: %s\n", escapeYAML(chatName))
		fmt.Fprintf(&b, "participants: [")
		for i, p := range participants {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(escapeYAML(p))
		}
		b.WriteString("]\n")
		fmt.Fprintf(&b, "message_count: %d\n", len(messages))
		fmt.Fprintf(&b, "type: %s\n", chatType)
		b.WriteString("---\n\n")
		fmt.Fprintf(&b, "# Чат с %s\n\n", chatName)

		for _, msg := range messages {
			ts := msg.Timestamp
			if len(ts) > 10 {
				ts = ts[:10]
			}
			text := msg.Text
			text = html.UnescapeString(text)
			text = strings.ReplaceAll(text, "\n", "\n  ")

			line := fmt.Sprintf("**%s** — %s: %s", ts, msg.From, text)
			if msg.Media != nil {
				line += " *(" + *msg.Media + ")*"
			}
			b.WriteString(line + "\n\n")
		}

		mdFile := filepath.Join(mdRoot, platform, sanitizeDir(chatName), "messages.md")
		if err := os.MkdirAll(filepath.Dir(mdFile), 0755); err != nil {
			fmt.Fprintf(os.Stderr, "chats import: mkdir %s: %v\n", filepath.Dir(mdFile), err)
			failed++
			continue
		}
		if err := os.WriteFile(mdFile, b.Bytes(), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "chats import: write %s: %v\n", mdFile, err)
			failed++
			continue
		}
		written++
	}
	fmt.Printf("chats import: %d chats written", written)
	if failed > 0 {
		fmt.Printf(", %d failed", failed)
	}
	fmt.Println()
	if failed > 0 {
		return 1
	}
	return 0
}

func readJSONL(path string) ([]Message, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	var messages []Message
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
		messages = append(messages, msg)
	}
	if err := scanner.Err(); err != nil {
		return messages, "", err
	}

	chatName := ""
	if len(messages) > 0 {
		nameCounts := make(map[string]int)
		for _, msg := range messages {
			nameCounts[msg.From]++
		}
		best := ""
		bestN := 0
		for name, n := range nameCounts {
			if name != "" && name != "unknown" && n > bestN {
				best = name
				bestN = n
			}
		}
		if best != "" {
			chatName = best
		}
	}
	return messages, chatName, nil
}

func collectParticipants(messages []Message) []string {
	seen := make(map[string]bool)
	var result []string
	for _, msg := range messages {
		if msg.From == "" || seen[msg.From] {
			continue
		}
		seen[msg.From] = true
		result = append(result, msg.From)
	}
	sort.Strings(result)
	return result
}

func escapeYAML(s string) string {
	if strings.ContainsAny(s, ":#,[]{}'\"") || strings.HasPrefix(s, "-") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

func sanitizeDir(name string) string {
	r := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
		" ", "_",
	)
	return strings.TrimSpace(r.Replace(name))
}

// validPlatforms lists the platforms chat import understands. Anything else
// is rejected rather than silently ingested under a wrong label.
var validPlatforms = map[string]bool{"telegram": true, "whatsapp": true}

func validPlatform(p string) bool {
	return validPlatforms[p]
}

// platformOf derives the platform label from the path layout
// <root>/<platform>/<chat>/messages.jsonl. Returns "" for an unknown or
// unrelatable path so the caller can reject it instead of mislabelling it.
func platformOf(path, root string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || len(rel) == 0 {
		return ""
	}
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
	if !validPlatform(parts[0]) {
		return ""
	}
	return parts[0]
}
