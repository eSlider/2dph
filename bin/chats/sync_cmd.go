package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func runSyncTelegram(args []string) int {
	fs := flag.NewFlagSet("chats sync telegram", flag.ContinueOnError)
	limit := fs.Int("limit", 0, "max messages per chat (0 = all)")
	phone := fs.String("phone", "", "phone number (default env TELEGRAM_PHONE)")
	help := fs.Bool("help", false, "")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *help {
		fmt.Fprintln(os.Stderr, "usage: chats sync telegram [--limit N] [--phone PHONE]")
		return 0
	}

	apiIDStr := envVar("TELEGRAM_API_ID", "")
	apiHash := envVar("TELEGRAM_API_HASH", "")
	sessionStr := envVar("TELEGRAM_SESSION_STRING", "")
	phoneNum := *phone
	if phoneNum == "" {
		phoneNum = envVar("TELEGRAM_PHONE", "")
	}
	if apiIDStr == "" || apiHash == "" || phoneNum == "" {
		fmt.Fprintln(os.Stderr, "chats: need TELEGRAM_API_ID, TELEGRAM_API_HASH, TELEGRAM_PHONE in env")
		return 2
	}
	apiID, err := strconv.Atoi(apiIDStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chats: invalid TELEGRAM_API_ID %q\n", apiIDStr)
		return 2
	}

	// Locate telegram-mcp directory
	mcpDirs := []string{
		envVar("TELEGRAM_MCP_DIR", ""),
		"/mnt/8TB/projects/eslider/mcp-servers/telegram-mcp",
		filepath.Join(os.Getenv("HOME"), "projects", "eSlider", "mcp-servers", "telegram-mcp"),
	}
	var mcpDir string
	for _, d := range mcpDirs {
		if d != "" {
			if _, err := os.Stat(filepath.Join(d, "main.py")); err == nil {
				mcpDir = d
				break
			}
		}
	}
	if mcpDir == "" {
		fmt.Fprintln(os.Stderr, "chats: telegram-mcp not found; set TELEGRAM_MCP_DIR")
		return 1
	}

	if sessionStr == "" {
		// Fallback: try to read from telegram-mcp .env
		envPath := filepath.Join(mcpDir, ".env")
		if data, err := os.ReadFile(envPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "TELEGRAM_SESSION_STRING=") {
					sessionStr = strings.TrimPrefix(line, "TELEGRAM_SESSION_STRING=")
					sessionStr = strings.Trim(sessionStr, "\"'")
					break
				}
			}
		}
	}
	if sessionStr == "" {
		// Also try the env file at /mnt/8TB/projects/eslider/mcp-servers/telegram.env
		envPath := "/mnt/8TB/projects/eslider/mcp-servers/telegram.env"
		if data, err := os.ReadFile(envPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "TELEGRAM_SESSION_STRING=") {
					sessionStr = strings.TrimPrefix(line, "TELEGRAM_SESSION_STRING=")
					sessionStr = strings.Trim(sessionStr, "\"'")
					break
				}
			}
		}
	}
	if sessionStr == "" {
		fmt.Fprintln(os.Stderr, "chats: TELEGRAM_SESSION_STRING not found in env or .env files")
		return 1
	}

	src := NewTelegramMCPSource(apiID, apiHash, phoneNum, sessionStr, mcpDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	if err := src.Sync(ctx, chatsDir(), *limit); err != nil {
		fmt.Fprintf(os.Stderr, "chats sync telegram: %v\n", err)
		return 1
	}
	fmt.Printf("chats sync telegram: completed in %s\n", time.Since(start).Round(time.Millisecond))
	return 0
}
