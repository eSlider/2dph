package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func checkLinkedInSession(userDataDir string) (bool, error) {
	cmd := exec.Command("uvx", "mcp-server-linkedin@latest",
		"--user-data-dir", userDataDir,
		"--no-auto-import",
		"--status",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return true, fmt.Errorf("status check: %w\n%s", err, string(out))
	}
	return !strings.Contains(string(out), "✅"), nil
}

func runSyncLinkedIn(args []string) int {
	fs := flag.NewFlagSet("chats sync linkedin", flag.ContinueOnError)
	limit := fs.Int("limit", 0, "max messages per conversation (0 = all)")
	help := fs.Bool("help", false, "")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *help {
		fmt.Fprintln(os.Stderr, "usage: chats sync linkedin [--limit N]")
		return 0
	}

	userDataDir := envVar("LINKEDIN_USER_DATA_DIR", "")
	if userDataDir == "" {
		home, _ := os.UserHomeDir()
		userDataDir = home + "/.linkedin-mcp/profile"
	}

	// Check session first
	loginNeeded, err := checkLinkedInSession(userDataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chats: linkedin status check: %v\n", err)
	}
	if loginNeeded {
		fmt.Fprintf(os.Stderr, "chats: LinkedIn session expired. Run:\n")
		fmt.Fprintf(os.Stderr, "  uvx mcp-server-linkedin@latest --user-data-dir %s --login\n", userDataDir)
		fmt.Fprintf(os.Stderr, "Then retry 'chats sync linkedin'\n")
		return 1
	}

	src := NewLinkedInMCPSource(userDataDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	if err := src.Sync(ctx, chatsDir(), *limit); err != nil {
		fmt.Fprintf(os.Stderr, "chats sync linkedin: %v\n", err)
		return 1
	}
	fmt.Printf("chats sync linkedin: completed in %s\n", time.Since(start).Round(time.Millisecond))
	return 0
}
