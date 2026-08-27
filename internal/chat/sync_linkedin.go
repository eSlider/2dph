package chat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	cliparse "github.com/eSlider/2dph/pkg/cli"
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

func RunSyncLinkedIn(args []string) int {
	f, err := parseLinkedInFlags(args)
	if err != nil {
		return cliparse.Fail(err)
	}
	limit := f.Limit
	refresh := f.Refresh

	userDataDir := envVar("LINKEDIN_USER_DATA_DIR", "")
	if userDataDir == "" {
		home, _ := os.UserHomeDir()
		userDataDir = home + "/.linkedin-mcp/profile"
	}

	if refresh {
		if code := refreshLinkedInSession(userDataDir); code != 0 {
			return code
		}
	}

	// Check session files first (no browser launch). A missing session is a
	// SKIP for the wave (exit 3), not a failure.
	if _, err := checkLinkedInSession(userDataDir); err != nil {
		fmt.Fprintf(os.Stderr, "chats: SKIP linkedin: %v (set LINKEDIN_USER_DATA_DIR or run `bin/chat/sync.go linkedin --refresh`)\n", err)
		return cliparse.ExitSkip
	}

	src := NewLinkedInMCPSource(userDataDir)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	start := time.Now()
	if err := src.Sync(ctx, Dir(), limit); err != nil {
		fmt.Fprintf(os.Stderr, "chats sync linkedin: %v\n", err)
		return 1
	}
	fmt.Printf("chats sync linkedin: completed in %s\n", time.Since(start).Round(time.Millisecond))
	return 0
}

// refreshLinkedInSession re-syncs the LinkedIn source session from the live
// webtop browser (CDP cookies + profile copy) — in-process, no helper script.
func refreshLinkedInSession(userDataDir string) int {
	root := filepath.Dir(userDataDir)
	if err := RefreshLinkedInSession(RefreshLinkedInSessionOpts{Root: root}); err != nil {
		fmt.Fprintf(os.Stderr, "chats: linkedin session refresh: %v\n", err)
		return 1
	}
	return 0
}
