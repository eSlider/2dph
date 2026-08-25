package chat

import (
	"path/filepath"
	"testing"

	cliparse "github.com/eSlider/2dph/pkg/cli"
)

// TestRunSyncTelegramSkipsWithoutCreds: missing TELEGRAM_* credentials is a
// SKIP (exit 3), not a failure — the wave prints SKIP instead of FAIL.
func TestRunSyncTelegramSkipsWithoutCreds(t *testing.T) {
	for _, k := range []string{
		"TELEGRAM_API_ID", "TELEGRAM_API_HASH", "TELEGRAM_PHONE",
		"TELEGRAM_SESSION_STRING", "TELEGRAM_MCP_DIR",
	} {
		t.Setenv(k, "")
	}
	if code := RunSyncTelegram(nil); code != cliparse.ExitSkip {
		t.Fatalf("exit = %d, want ExitSkip(%d)", code, cliparse.ExitSkip)
	}
}

// TestRunSyncLinkedInSkipsWithoutSession: no LinkedIn session files (default
// or LINKEDIN_USER_DATA_DIR) is a SKIP, not a failure.
func TestRunSyncLinkedInSkipsWithoutSession(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-profile")
	t.Setenv("LINKEDIN_USER_DATA_DIR", missing)
	if code := RunSyncLinkedIn(nil); code != cliparse.ExitSkip {
		t.Fatalf("exit = %d, want ExitSkip(%d)", code, cliparse.ExitSkip)
	}
}
