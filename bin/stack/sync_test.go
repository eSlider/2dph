// Tests for the stack wave orchestrator (issue #195): runner build tags,
// chats step planning (telegram+linkedin), and SKIP-on-missing-creds semantics.
package main

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/eSlider/2dph/pkg/repo"
)

// TestRunnerChatsTag asserts bin/chat/sync.go is built with the chats_sync
// tag, matching its //go:build chats_sync constraint (issue #195).
func TestRunnerChatsTag(t *testing.T) {
	cmd, err := runner("bin/chat/sync.go", []string{"telegram"})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	if !strings.HasSuffix(cmd.Args[0], "2dph-stack-bin_chat_sync.go") {
		t.Errorf("argv[0] = %q, want per-tool temp binary", cmd.Args[0])
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "telegram" {
		t.Errorf("argv = %v, want [bin telegram]", cmd.Args)
	}
	argv := buildArgv("bin/chat/sync.go", toolTags("bin/chat/sync.go"))
	joined := strings.Join(argv, " ")
	for _, want := range []string{"go", "build", "-tags=chats_sync", "bin/chat/sync.go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("build argv %v missing %q", argv, want)
		}
	}
}

// TestRunnerExistingTags keeps the established tag scheme intact.
func TestRunnerExistingTags(t *testing.T) {
	cases := []struct{ tool, want string }{
		{"bin/mail/import.go", "mail_import"},
		{"bin/chat/sync.go", "chats_sync"},
		{"bin/onlyoffice/import-contact.go", "onlyoffice_import_contact"},
		{"bin/onlyoffice/reconcile-contact.go", "onlyoffice_reconcile_contact"},
	}
	for _, c := range cases {
		if got := toolTags(c.tool); got != c.want {
			t.Errorf("toolTags(%s) = %q, want %q", c.tool, got, c.want)
		}
		if joined := strings.Join(buildArgv(c.tool, c.want), " "); !strings.Contains(joined, "-tags="+c.want) {
			t.Errorf("buildArgv(%s) = %s, missing -tags=%s", c.tool, joined, c.want)
		}
	}
	// brain/* go through the Zig CGO wrapper, not the temp-binary path.
	cmd, err := runner("bin/brain/import-git.go", []string{"--root", "/x"})
	if err != nil {
		t.Fatalf("runner(brain): %v", err)
	}
	if cmd.Args[0] != "bash" {
		t.Errorf("brain runner argv[0] = %q, want bash", cmd.Args[0])
	}
}

// TestPlanChatsStep checks the chats step stays a single logical step name but
// plans two sub-commands: telegram then linkedin.
func TestPlanChatsStep(t *testing.T) {
	want := [][]string{
		{"bin/chat/sync.go", "telegram"},
		{"bin/chat/sync.go", "linkedin"},
	}
	steps := planSteps(true, false, "", "")
	var got []string
	for i := range steps {
		if steps[i].name != "chats" {
			continue
		}
		if steps[i].skip != "" {
			t.Errorf("chats skip = %q, want empty with --with-chats", steps[i].skip)
		}
		if !reflect.DeepEqual(steps[i].cmds, want) {
			t.Errorf("chats cmds = %v, want %v", steps[i].cmds, want)
		}
		got = append(got, steps[i].cmds[0][1], steps[i].cmds[1][1])
	}
	if len(got) == 0 {
		t.Fatal("no chats step planned")
	}
	if !reflect.DeepEqual(got, []string{"telegram", "linkedin"}) {
		t.Errorf("chats sub-commands = %v, want [telegram linkedin]", got)
	}

	// Without --with-chats the whole step is skipped, order kept for --only.
	skipped := planSteps(false, false, "", "")
	for i := range skipped {
		if skipped[i].name == "chats" && skipped[i].skip != "--with-chats" {
			t.Errorf("chats skip = %q, want --with-chats", skipped[i].skip)
		}
	}
}

// TestPlanMailIndexStep checks the mail-index step (issue #199): planned right
// after mail-import with --with-mail, skipped (never FAIL) without it.
func TestPlanMailIndexStep(t *testing.T) {
	steps := planSteps(false, true, "", "")
	idx := -1
	importAt := -1
	for i := range steps {
		if steps[i].name == "mail-import" {
			importAt = i
		}
		if steps[i].name == "mail-index" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("no mail-index step planned with --with-mail")
	}
	if importAt < 0 || idx != importAt+1 {
		t.Errorf("mail-index at %d, want exactly after mail-import (%d)", idx, importAt)
	}
	if steps[idx].skip != "" {
		t.Errorf("mail-index skip = %q, want empty with --with-mail", steps[idx].skip)
	}
	want := [][]string{{"bin/brain/index.go", "--skip", "--with-mail"}}
	if !reflect.DeepEqual(steps[idx].cmds, want) {
		t.Errorf("mail-index cmds = %v, want %v", steps[idx].cmds, want)
	}

	// Without --with-mail the step must SKIP, not FAIL (wave still green).
	skipped := planSteps(false, false, "", "")
	for i := range skipped {
		if skipped[i].name == "mail-index" && skipped[i].skip != "--with-mail" {
			t.Errorf("mail-index skip = %q, want --with-mail", skipped[i].skip)
		}
	}
}

// TestRunnerBrainIndexTags pins the Zig CGO build tags for bin/brain/index.go:
// its //go:build line requires brain_index in addition to system_ladybug.
func TestRunnerBrainIndexTags(t *testing.T) {
	cmd, err := runner("bin/brain/index.go", []string{"--skip", "--with-mail"})
	if err != nil {
		t.Fatalf("runner(index): %v", err)
	}
	if cmd.Args[0] != "bash" {
		t.Errorf("brain runner argv[0] = %q, want bash", cmd.Args[0])
	}
	joined := strings.Join(cmd.Args, " ")
	for _, want := range []string{
		"cgo/zig", "go run", "-tags=system_ladybug,brain_index",
		"bin/brain/index.go", "--skip", "--with-mail",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("runner argv %q missing %q", joined, want)
		}
	}
}

// TestExitSkipCode pins the wave's SKIP classification to exit code 3.
func TestExitSkipCode(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 3").Run()
	if err == nil {
		t.Fatal("expected exit error")
	}
	if !exitSkip(err) {
		t.Error("exit code 3 must classify as SKIP")
	}
	err = exec.Command("sh", "-c", "exit 1").Run()
	if exitSkip(err) {
		t.Error("exit code 1 must not classify as SKIP")
	}
}

// TestRunWaveChatsSkipsWithoutCreds is a use-case test: the real wave with
// --only chats --with-chats and no chat credentials must SKIP both
// sub-commands and finish with failures=0 (exit 0), never FAIL.
func TestRunWaveChatsSkipsWithoutCreds(t *testing.T) {
	for _, k := range []string{
		"TELEGRAM_API_ID", "TELEGRAM_API_HASH", "TELEGRAM_PHONE",
		"TELEGRAM_SESSION_STRING", "TELEGRAM_MCP_DIR", "LINKEDIN_USER_DATA_DIR",
	} {
		t.Setenv(k, "")
	}
	// The wave resolves bin/… and var/… relative to the repo root.
	root := repo.Root()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })

	code := run([]string{"--only", "chats", "--with-chats"})
	if code != 0 {
		t.Fatalf("wave exit = %d, want 0 (SKIP, not FAIL)", code)
	}
}
