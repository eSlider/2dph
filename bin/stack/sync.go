// usr/bin/env go run "$0" "$@"; exit
//
// bin/stack/sync.go - deterministic sync wave: run every ingest/reconcile step
// in fixed order, stream each tool's output, print a summary, exit non-zero on
// any failure.
//
//	./bin/stack/sync.go                          # default wave
//	./bin/stack/sync.go --with-chats --contacts ~/contacts.vcf
//	./bin/stack/sync.go --only mail,crm          # subset, fixed order kept
//
// Steps (fixed order):
//  1. mail-sync      bin/mail/sync.go        (skipped without creds)
//  2. mail-import    bin/mail/import.go
//  3. chats          chat/sync telegram+linkedin   (--with-chats)
//  4. contact-brain  brain/import-contact.go       (--contacts PATH)
//  5. git-brain      brain/import-git.go           (--git-root DIR)
//  6. contact-crm    onlyoffice/import-contact.go  (--contacts PATH)
//
// Idempotent by construction: every step is a reconcile/upsert. Bulk rebuild
// is deliberately NOT part of the wave (expensive) — use brain/index.go.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	cliparse "github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/repo"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		only      string
		contacts  string
		gitRoot   string
		withChats bool
		dryRun    bool
	)
	p := cliparse.New("stack-sync")
	p.Description = "deterministic sync wave: mail/chats/contacts/git → brain + OO CRM"
	p.String(&only, "", "only", "comma-separated subset: mail,chats,contacts,git,crm")
	p.String(&contacts, "", "contacts", "address-book file/dir for brain+CRM reconcile")
	p.String(&gitRoot, "", "git-root", "dir of git repos to import into the brain")
	p.Bool(&withChats, "", "with-chats", "include telegram/linkedin chat sync")
	p.Bool(&dryRun, "", "dry-run", "print the wave without executing")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}

	want := map[string]bool{}
	if only != "" {
		for _, s := range strings.Split(only, ",") {
			want[strings.TrimSpace(s)] = true
		}
	}
	included := func(name string) bool { return len(want) == 0 || want[name] }

	steps := []struct {
		name string
		args []string
		skip string
	}{
		{"mail", []string{"bin/mail/sync.go"}, ""},
		{"mail-import", []string{"bin/mail/import.go", "--from-raw", "var/mail"}, ""},
		{"chats", []string{"bin/chat/sync.go"}, skipUnless(withChats, "--with-chats")},
		{"contact-brain", contactStep("bin/brain/import-contact.go", contacts), skipUnless(contacts != "", "--contacts")},
		{"git-brain", gitStep(gitRoot), skipUnless(gitRoot != "", "--git-root")},
		{"contact-crm", contactStep("bin/onlyoffice/import-contact.go", contacts), skipUnless(contacts != "", "--contacts")},
	}

	var failed int
	fmt.Fprintln(os.Stderr, "stack-sync: wave start")

	// Ladybug allows a single writer: local brain-serve/brain-search processes
	// hold kb.lbug. Quiesce them around write steps, restart after the wave.
	writeSteps := map[string]bool{"contact-brain": true, "git-brain": true}
	needWrite := false
	for _, st := range steps {
		if writeSteps[st.name] && included(st.name) && st.skip == "" && !dryRun {
			needWrite = true
		}
	}
	stopped := false
	if needWrite && brainServing() {
		fmt.Fprintln(os.Stderr, "  quiesce        STOP brain-serve/search (single-writer)")
		quiesceBrain()
		stopped = true
	}
	defer func() {
		if stopped {
			restartBrain()
		}
	}()

	for _, st := range steps {
		if !included(st.name) {
			continue
		}
		if st.skip != "" {
			fmt.Fprintf(os.Stderr, "  %-14s SKIP (%s)\n", st.name, st.skip)
			continue
		}
		if dryRun {
			fmt.Fprintf(os.Stderr, "  %-14s DRY %v\n", st.name, st.args)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-14s RUN %v\n", st.name, st.args)
		cmd := runner(st.args[0], st.args[1:])
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			failed++
			fmt.Fprintf(os.Stderr, "  %-14s FAIL %v\n", st.name, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "  %-14s OK\n", st.name)
	}
	fmt.Fprintf(os.Stderr, "stack-sync: wave done (failures=%d)\n", failed)
	if failed > 0 {
		return 1
	}
	return 0
}

// runner builds the exec for a shebang tool. The `//usr/bin/env` trick is not
// kernel-executable, so we reproduce each tool's own invocation:
//   - brain/* need Zig CGO (bin/cgo/zig go run -tags=system_ladybug)
//   - some tools carry build tags (mail/import → mail_import)
//   - everything else is plain `go run`
func runner(tool string, args []string) *exec.Cmd {
	dir := "."
	if i := strings.LastIndex(tool, "/"); i > 0 {
		dir = tool[:i]
	}
	switch {
	case strings.HasPrefix(tool, "bin/brain/"):
		script := fmt.Sprintf(`exec "%s/../cgo/zig" go run -tags=system_ladybug %q "$@"`, dir, tool)
		argv := append([]string{"-c", script, "--"}, args...)
		return exec.Command("bash", argv...)
	case tool == "bin/mail/import.go":
		return exec.Command("go", append([]string{"run", "-tags=mail_import", tool}, args...)...)
	case strings.HasPrefix(tool, "bin/onlyoffice/"):
		tag := "onlyoffice_" + strings.ReplaceAll(strings.TrimSuffix(filepath.Base(tool), ".go"), "-", "_")
		return exec.Command("go", append([]string{"run", "-tags=" + tag, tool}, args...)...)
	default:
		return exec.Command("go", append([]string{"run", tool}, args...)...)
	}
}

func contactStep(tool, contacts string) []string {
	a := []string{tool, "--sources", contacts}
	return a
}

func gitStep(root string) []string {
	return []string{"bin/brain/import-git.go", "--root", root}
}

func skipUnless(cond bool, what string) string {
	if cond {
		return ""
	}
	return what
}

// brainServing reports whether local built brain processes are running.
func brainServing() bool {
	out, err := exec.Command("pgrep", "-f", `bin-build/brain-(serve|search)`).Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// quiesceBrain SIGTERMs the local brain serve/search processes and waits for
// them to release kb.lbug (bounded).
func quiesceBrain() {
	_ = exec.Command("pkill", "-TERM", "-f", `bin-build/brain-serve`).Run()
	_ = exec.Command("pkill", "-TERM", "-f", `bin-build/brain-search serve`).Run()
	for i := 0; i < 30; i++ {
		if !brainServing() {
			time.Sleep(500 * time.Millisecond) // let the kernel release fds
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "  quiesce        WARN brain processes still running")
}

// restartBrain brings the API + embedding daemon back via the stack script.
func restartBrain() {
	fmt.Fprintln(os.Stderr, "  restart        brain-serve/search")
	root := repo.Root()
	cmd := exec.Command("bash", filepath.Join(root, "bin", "stack", "serve-brain"))
	cmd.Dir = root
	_ = cmd.Run()
}
