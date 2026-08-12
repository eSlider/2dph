package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func runImport(args []string) int {
	fs := flag.NewFlagSet("chats import", flag.ContinueOnError)
	help := fs.Bool("help", false, "")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *help {
		fmt.Fprintln(os.Stderr, "usage: chats import")
		return 0
	}

	root := chatsDir()
	mdRoot := filepath.Join(root, "md")
	glob := filepath.Join(root, "telegram", "*", "messages.jsonl")

	matches, err := filepath.Glob(glob)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chats import: glob %s: %v\n", glob, err)
		return 1
	}
	if len(matches) == 0 {
		fmt.Fprintf(os.Stderr, "chats import: no messages.jsonl found under %s\n", root)
		return 1
	}

	written := 0
	failed := 0
	for _, jsonlPath := range matches {
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
		fmt.Fprintf(&b, "platform: telegram\n")
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

		mdFile := filepath.Join(mdRoot, "telegram", sanitizeDir(chatName), "messages.md")
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
