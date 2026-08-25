package mailsync

import (
	"path/filepath"
	"testing"

	cliparse "github.com/eSlider/2dph/pkg/cli"
)

// TestParseCLINoOnlyOfficeCredsSkips: the wave docs promise "mail skipped
// without creds"; missing ONLYOFFICE_* must surface as ExitSkip, not a usage
// error (issue #195).
func TestParseCLINoOnlyOfficeCredsSkips(t *testing.T) {
	for _, k := range []string{
		"ONLYOFFICE_URL", "ONLYOFFICE_USER", "ONLYOFFICE_PASS",
		"OO_URL", "OO_USER", "OO_PASSWORD",
	} {
		t.Setenv(k, "")
	}
	_, code, err := ParseCLI([]string{"--source", "onlyoffice", "--env", filepath.Join(t.TempDir(), "empty.env")})
	if err == nil {
		t.Fatal("expected an error for missing creds")
	}
	if code != cliparse.ExitSkip {
		t.Fatalf("code = %d, want ExitSkip(%d)", code, cliparse.ExitSkip)
	}
}

// TestParseCLIMissingIMAPSkips mirrors the same convention for the IMAP source.
func TestParseCLIMissingIMAPSkips(t *testing.T) {
	for _, k := range []string{"IMAP_HOST", "IMAP_USER", "IMAP_PASSWORD", "MAIL_IMAP_HOST", "MAIL_IMAP_USER", "MAIL_IMAP_PASSWORD"} {
		t.Setenv(k, "")
	}
	_, code, err := ParseCLI([]string{"--source", "imap", "--env", filepath.Join(t.TempDir(), "empty.env")})
	if err == nil {
		t.Fatal("expected an error for missing creds")
	}
	if code != cliparse.ExitSkip {
		t.Fatalf("code = %d, want ExitSkip(%d)", code, cliparse.ExitSkip)
	}
}
