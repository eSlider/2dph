package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func checkLinkedInSession(userDataDir string) (bool, error) {
	// Validate the source-session files without launching a browser. A full
	// `--status` run spawns Chromium and loads /feed/, doubling the automation
	// exposed to LinkedIn (429 rate limits) before the sync even starts.
	root := filepath.Dir(userDataDir)
	sessionFiles := []string{
		filepath.Join(root, "source-state.json"),
		filepath.Join(root, "cookies.json"),
		filepath.Join(userDataDir, "Default", "Cookies"),
	}
	for _, f := range sessionFiles {
		if _, err := os.Stat(f); err != nil {
			return true, fmt.Errorf("missing session file %s", f)
		}
	}
	return false, nil
}

func runSyncLinkedIn(args []string) int {
	fs := flag.NewFlagSet("chats sync linkedin", flag.ContinueOnError)
	limit := fs.Int("limit", 0, "max messages per conversation (0 = all)")
	refresh := fs.Bool("refresh", false, "refresh session from live webtop browser before sync")
	help := fs.Bool("help", false, "")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *help {
		fmt.Fprintln(os.Stderr, "usage: chats sync linkedin [--limit N] [--refresh]")
		return 0
	}

	userDataDir := envVar("LINKEDIN_USER_DATA_DIR", "")
	if userDataDir == "" {
		home, _ := os.UserHomeDir()
		userDataDir = home + "/.linkedin-mcp/profile"
	}

	if *refresh {
		if code := refreshLinkedInSession(userDataDir); code != 0 {
			return code
		}
	}

	// Check session files first (no browser launch).
	loginNeeded, err := checkLinkedInSession(userDataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chats: linkedin status check: %v\n", err)
	}
	if loginNeeded {
		fmt.Fprintf(os.Stderr, "chats: LinkedIn session missing. Run:\n")
		fmt.Fprintf(os.Stderr, "  chats sync linkedin --refresh\n")
		fmt.Fprintf(os.Stderr, "or point LINKEDIN_USER_DATA_DIR at a valid session\n")
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

// refreshLinkedInSession re-syncs the LinkedIn source session from the live
// webtop browser via the vendored refresh-linkedin-session helper.
func refreshLinkedInSession(userDataDir string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "chats: resolve executable: %v\n", err)
		return 1
	}
	helper := filepath.Join(filepath.Dir(exe), "refresh-linkedin-session")
	if _, err := os.Stat(helper); err != nil {
		// Fall back to the source tree helper next to this command file.
		helper = "bin/chats/refresh-linkedin-session"
	}
	root := filepath.Dir(userDataDir)
	cmd := exec.Command(helper, "--root", root)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "chats: linkedin session refresh: %v\n", err)
		return 1
	}
	return 0
}
