// System tests for bin/chats.
//
// These are integration tests using real data and real Telegram API (when
// credentials are available). They follow the TDD workflow pattern:
// sync → import → facts → verify.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestChatsImport validates JSONL → MD conversion with a synthetic fixture.
func TestChatsImport(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "var", "chats")
	chatDir := filepath.Join(root, "telegram", "test_user_123")
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		t.Fatal(err)
	}

	jsonlPath := filepath.Join(chatDir, "messages.jsonl")
	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	messages := []Message{
		{ID: "tg_1", Timestamp: "2026-01-15T10:30:00Z", From: "Alice", Text: "Hello!", Platform: "telegram"},
		{ID: "tg_2", Timestamp: "2026-01-15T10:31:00Z", From: "Bob", Text: "Hi Alice, my phone is +34 612 345 678", Platform: "telegram"},
		{ID: "tg_3", Timestamp: "2026-01-15T10:32:00Z", From: "Alice", Text: "Check my LinkedIn: https://linkedin.com/in/alice-test", Platform: "telegram"},
		{ID: "tg_4", Timestamp: "2026-01-15T10:33:00Z", From: "Bob", Text: "My email is bob@example.com, working on Project X", Platform: "telegram"},
	}
	for _, m := range messages {
		if err := enc.Encode(m); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })
	t.Setenv("KB_ROOT", dir)

	exitCode := runImport([]string{})
	if exitCode != 0 {
		t.Fatalf("import exit code %d", exitCode)
	}

	mdGlob := filepath.Join(root, "md", "telegram", "*", "messages.md")
	matches, err := filepath.Glob(mdGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("no markdown files created by import")
	}

	mdData, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	content := string(mdData)

	if !strings.Contains(content, "Alice") {
		t.Error("markdown missing sender name 'Alice'")
	}
	if !strings.Contains(content, "2026-01-15") {
		t.Error("markdown missing date")
	}
	if !strings.Contains(content, "---") {
		t.Error("markdown missing YAML frontmatter")
	}
}

// TestChatsFacts validates fact extraction from JSONL fixture.
func TestChatsFacts(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "var", "chats")
	chatDir := filepath.Join(root, "telegram", "test_user_facts")
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		t.Fatal(err)
	}

	jsonlPath := filepath.Join(chatDir, "messages.jsonl")
	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	messages := []Message{
		{ID: "tg_10", Timestamp: "2026-06-01T12:00:00Z", From: "Charlie", Text: "Call me at +1 555 123 4567", Platform: "telegram"},
		{ID: "tg_11", Timestamp: "2026-06-01T12:01:00Z", From: "Charlie", Text: "My LinkedIn is linkedin.com/in/charlie-dev", Platform: "telegram"},
		{ID: "tg_12", Timestamp: "2026-06-01T12:02:00Z", From: "Charlie", Text: "Email: charlie@dev.com", Platform: "telegram"},
		{ID: "tg_13", Timestamp: "2026-06-01T12:03:00Z", From: "Charlie", Text: "I work at Acme Corp on Project Mercury", Platform: "telegram"},
	}
	for _, m := range messages {
		if err := enc.Encode(m); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()

	facts, _ := extractFacts(jsonlPath, "test_user_facts")
	if len(facts) == 0 {
		t.Fatal("expected facts, got none")
	}

	types := make(map[string]int)
	for _, f := range facts {
		types[f.FactType]++
	}
	if types["phone"] < 1 {
		t.Errorf("expected >=1 phone fact, got %d", types["phone"])
	}
	if types["email"] < 1 {
		t.Errorf("expected >=1 email fact, got %d", types["email"])
	}
	if types["linkedin"] < 1 {
		t.Errorf("expected >=1 linkedin fact, got %d", types["linkedin"])
	}
	if types["skill"] < 1 {
		t.Errorf("expected >=1 skill fact, got %d", types["skill"])
	}
}

// TestChatsImportEmptyDir tests that import handles no JSONL gracefully.
func TestChatsImportEmpty(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })
	t.Setenv("KB_ROOT", dir)

	exitCode := runImport([]string{})
	if exitCode == 0 {
		t.Fatal("expected non-zero exit for empty data dir")
	}
}

// TestChatsRoundTrip creates a synthetic JSONL, imports it, then verifies
// the markdown structure is parseable and contains YAML frontmatter.
func TestChatsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "var", "chats")
	chatDir := filepath.Join(root, "telegram", "rt_user")
	if err := os.MkdirAll(chatDir, 0755); err != nil {
		t.Fatal(err)
	}

	jsonlPath := filepath.Join(chatDir, "messages.jsonl")
	f, err := os.Create(jsonlPath)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	enc.Encode(Message{ID: "tg_100", Timestamp: "2026-07-01T08:00:00Z", From: "Diana", Text: "Hey", Platform: "telegram"})
	enc.Encode(Message{ID: "tg_101", Timestamp: "2026-07-01T08:01:00Z", From: "Diana", Text: "How are you?", Platform: "telegram"})
	f.Close()

	cwd, _ := os.Getwd()
	os.Chdir(dir)
	t.Cleanup(func() { os.Chdir(cwd) })
	t.Setenv("KB_ROOT", dir)

	if code := runImport([]string{}); code != 0 {
		t.Fatalf("import exit %d", code)
	}

	mdGlob := filepath.Join(root, "md", "telegram", "*", "messages.md")
	matches, _ := filepath.Glob(mdGlob)
	if len(matches) == 0 {
		t.Fatal("no markdown produced")
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)

	if !strings.HasPrefix(content, "---") {
		t.Error("markdown should start with YAML frontmatter delimiter")
	}
	if !strings.Contains(content, "platform: telegram") {
		t.Error("markdown should contain platform field")
	}
	if !strings.Contains(content, "message_count: 2") {
		t.Error("markdown should contain correct message count")
	}
	if !strings.Contains(content, "Diana") {
		t.Error("markdown should contain participants")
	}
}
