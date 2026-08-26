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
//  3. mail-index     bin/brain/index.go --skip --with-mail  (--with-mail)
//  4. chats          chat/sync telegram+linkedin   (--with-chats; each platform
//     SKIPs on exit code 3 when its creds/session are missing)
//  5. contact-brain  brain/import-contact.go       (--contacts PATH)
//  6. git-brain      brain/import-git.go           (--git-root DIR)
//  7. contact-crm    onlyoffice/import-contact.go  (--contacts PATH)
//
// Idempotent by construction: every step is a reconcile/upsert. Bulk rebuild
// is deliberately NOT part of the wave (expensive) — use brain/index.go.
package main

import (
	"errors"
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
		withMail  bool
		dryRun    bool
	)
	p := cliparse.New("stack-sync")
	p.Description = "deterministic sync wave: mail/chats/contacts/git → brain + OO CRM"
	p.String(&only, "", "only", "comma-separated subset: mail,mail-import,mail-index,chats,contacts,git,crm")
	p.String(&contacts, "", "contacts", "address-book file/dir for brain+CRM reconcile")
	p.String(&gitRoot, "", "git-root", "dir of git repos to import into the brain")
	p.Bool(&withChats, "", "with-chats", "include telegram/linkedin chat sync")
	p.Bool(&withMail, "", "with-mail", "index mail leafs into the brain after mail-import")
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

	steps := planSteps(withChats, withMail, contacts, gitRoot)

	var failed int
	fmt.Fprintln(os.Stderr, "stack-sync: wave start")

	// Ladybug allows a single writer: local brain-serve/brain-search processes
	// and the compose brain container (D25) all hold the same host kb.lbug.
	// Quiesce them around write steps, restart after the wave.
	writeSteps := map[string]bool{"contact-brain": true, "git-brain": true, "mail-index": true}
	needWrite := false
	for _, st := range steps {
		if writeSteps[st.name] && included(st.name) && st.skip == "" && !dryRun {
			needWrite = true
		}
	}
	stopped := false
	containerStopped := false
	if needWrite && brainServing() {
		fmt.Fprintln(os.Stderr, "  quiesce        STOP brain-serve/search (single-writer)")
		containerStopped = quiesceBrain()
		stopped = true
	}
	defer func() {
		if stopped {
			restartBrain(containerStopped)
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
			fmt.Fprintf(os.Stderr, "  %-14s DRY %v\n", st.name, st.cmds)
			continue
		}
		var skipped int
		stepFailed := false
		for _, cmdArgs := range st.cmds {
			fmt.Fprintf(os.Stderr, "  %-14s RUN %v\n", st.name, cmdArgs)
			cmd, err := runner(cmdArgs[0], cmdArgs[1:])
			if err != nil {
				stepFailed = true
				failed++
				fmt.Fprintf(os.Stderr, "  %-14s FAIL %v\n", st.name, err)
				break
			}
			cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
			if err := cmd.Run(); err != nil {
				if exitSkip(err) {
					skipped++
					fmt.Fprintf(os.Stderr, "  %-14s SKIP %v\n", st.name, err)
					continue
				}
				stepFailed = true
				failed++
				fmt.Fprintf(os.Stderr, "  %-14s FAIL %v\n", st.name, err)
				break
			}
		}
		if stepFailed {
			continue
		}
		switch {
		case skipped == len(st.cmds):
			fmt.Fprintf(os.Stderr, "  %-14s SKIP (all %d sub-command(s))\n", st.name, skipped)
		case skipped > 0:
			fmt.Fprintf(os.Stderr, "  %-14s OK (%d sub-command(s) skipped)\n", st.name, skipped)
		default:
			fmt.Fprintf(os.Stderr, "  %-14s OK\n", st.name)
		}
	}
	fmt.Fprintf(os.Stderr, "stack-sync: wave done (failures=%d)\n", failed)
	if failed > 0 {
		return 1
	}
	return 0
}

// step is one logical wave step. A step may run several tools sequentially
// (chats = telegram + linkedin) while keeping a single logical name for
// --only and the summary.
type step struct {
	name string
	cmds [][]string
	skip string
}

// planSteps builds the fixed-order wave. Step names are the public --only
// vocabulary; cmds are the actual tool invocations.
func planSteps(withChats, withMail bool, contacts, gitRoot string) []step {
	return []step{
		{name: "mail", cmds: [][]string{{"bin/mail/sync.go"}}},
		{name: "mail-import", cmds: [][]string{{"bin/mail/import.go", "--from-raw", "var/corpus/mail"}}},
		{name: "mail-index", cmds: [][]string{{"bin/brain/index.go", "--skip", "--with-mail"}}, skip: skipUnless(withMail, "--with-mail")},
		{name: "chats", cmds: [][]string{{"bin/chat/sync.go", "telegram"}, {"bin/chat/sync.go", "linkedin"}}, skip: skipUnless(withChats, "--with-chats")},
		{name: "contact-brain", cmds: [][]string{contactStep("bin/brain/import-contact.go", contacts)}, skip: skipUnless(contacts != "", "--contacts")},
		{name: "git-brain", cmds: [][]string{gitStep(gitRoot)}, skip: skipUnless(gitRoot != "", "--git-root")},
		{name: "contact-crm", cmds: [][]string{contactStep("bin/onlyoffice/import-contact.go", contacts)}, skip: skipUnless(contacts != "", "--contacts")},
	}
}

// exitSkip reports whether err is a process exit with cliparse.ExitSkip —
// the tool's way of saying "not configured, skip me" instead of failing.
func exitSkip(err error) bool {
	var ee *exec.ExitError
	return errors.As(err, &ee) && ee.ExitCode() == cliparse.ExitSkip
}

// runner builds the exec for a shebang tool. The `//usr/bin/env` trick is not
// kernel-executable, so we reproduce each tool's own invocation:
//   - brain/* need Zig CGO (bin/cgo/zig go run -tags=system_ladybug)
//   - everything else is compiled to a temp binary and exec'd directly.
//
// Tools run as built binaries (not `go run`) so their real exit code
// survives: `go run` collapses any child failure to exit status 1, which
// would turn a deliberate SKIP (cliparse.ExitSkip) into a wave FAIL.
func runner(tool string, args []string) (*exec.Cmd, error) {
	dir := "."
	if i := strings.LastIndex(tool, "/"); i > 0 {
		dir = tool[:i]
	}
	if strings.HasPrefix(tool, "bin/brain/") {
		script := fmt.Sprintf(`exec "%s/../cgo/zig" go run -tags=%s %q "$@"`, dir, brainTags(tool), tool)
		argv := append([]string{"-c", script, "--"}, args...)
		return exec.Command("bash", argv...), nil
	}
	bin, err := buildTool(tool, toolTags(tool))
	if err != nil {
		return nil, err
	}
	return exec.Command(bin, args...), nil
}

// toolTags maps a wave tool to the build tag its source declares
// (//go:build line); "" means no tag.
func toolTags(tool string) string {
	switch tool {
	case "bin/mail/import.go":
		return "mail_import"
	case "bin/chat/sync.go":
		return "chats_sync"
	}
	if strings.HasPrefix(tool, "bin/onlyoffice/") {
		return "onlyoffice_" + strings.ReplaceAll(strings.TrimSuffix(filepath.Base(tool), ".go"), "-", "_")
	}
	return ""
}

// brainTags maps a brain wave tool to its Zig CGO build tags. Most brain
// tools need only system_ladybug; bin/brain/index.go additionally declares
// brain_index in its //go:build line, so the runner must pass it or
// `go run` finds no files for the tool.
func brainTags(tool string) string {
	switch tool {
	case "bin/brain/index.go":
		return "system_ladybug,brain_index"
	default:
		return "system_ladybug"
	}
}

// buildArgv returns the `go build` argv for tool with tags ("" = none).
func buildArgv(tool, tags string) []string {
	bin := filepath.Join(os.TempDir(), "2dph-stack-"+strings.ReplaceAll(tool, "/", "_"))
	argv := []string{"go", "build"}
	if tags != "" {
		argv = append(argv, "-tags="+tags)
	}
	return append(argv, "-o", bin, tool)
}

// buildTool compiles tool into a per-tool temp binary and returns its path.
// The build runs from the repo root so tools resolve regardless of CWD.
func buildTool(tool, tags string) (string, error) {
	argv := buildArgv(tool, tags)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = repo.Root()
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build %s: %w (%s)", tool, err, strings.TrimSpace(string(out)))
	}
	for i, a := range argv {
		if a == "-o" {
			return argv[i+1], nil
		}
	}
	return "", fmt.Errorf("build %s: internal error: no -o in argv", tool)
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

// brainServing reports whether the brain holds kb.lbug: local bin-build
// processes or the compose brain container (D25 binds host var/ into the
// container, so its serve process locks the same DB file).
func brainServing() bool {
	out, err := exec.Command("pgrep", "-f", `bin-build/brain-(serve|search)`).Output()
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		return true
	}
	return brainContainer()
}

// brainContainer reports whether the compose brain service is running.
func brainContainer() bool {
	out, err := exec.Command("docker", "compose",
		"-f", filepath.Join(repo.Root(), "compose.yaml"),
		"--project-directory", repo.Root(),
		"ps", "-q", "brain").Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// quiesceBrain SIGTERMs local brain serve/search processes and stops the
// compose brain container, then waits for kb.lbug to be released (bounded).
// Returns whether the container was stopped (restartBrain needs to know).
func quiesceBrain() (containerStopped bool) {
	_ = exec.Command("pkill", "-TERM", "-f", `bin-build/brain-serve`).Run()
	_ = exec.Command("pkill", "-TERM", "-f", `bin-build/brain-search serve`).Run()
	if brainContainer() {
		stop := exec.Command("docker", "compose",
			"-f", filepath.Join(repo.Root(), "compose.yaml"),
			"--project-directory", repo.Root(),
			"stop", "brain")
		stop.Stdout, stop.Stderr = os.Stderr, os.Stderr
		if err := stop.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  quiesce        WARN docker compose stop brain: %v\n", err)
		} else {
			containerStopped = true
		}
	}
	for i := 0; i < 30; i++ {
		if !brainServing() {
			time.Sleep(500 * time.Millisecond) // let the kernel release fds
			return containerStopped
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "  quiesce        WARN brain still running")
	return containerStopped
}

// restartBrain brings the brain back: the compose container if the wave
// stopped it, otherwise the local API + embedding daemon via serve-brain.
func restartBrain(containerStopped bool) {
	fmt.Fprintln(os.Stderr, "  restart        brain-serve/search")
	if containerStopped {
		start := exec.Command("docker", "compose",
			"-f", filepath.Join(repo.Root(), "compose.yaml"),
			"--project-directory", repo.Root(),
			"start", "brain")
		start.Stdout, start.Stderr = os.Stderr, os.Stderr
		if err := start.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "  restart        WARN docker compose start brain: %v\n", err)
		}
		return
	}
	root := repo.Root()
	cmd := exec.Command("bash", filepath.Join(root, "bin", "stack", "serve-brain"))
	cmd.Dir = root
	_ = cmd.Run()
}
