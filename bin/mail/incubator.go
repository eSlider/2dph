//usr/bin/env go run -tags=mail_incubator "$0" "$@"; exit
//go:build mail_incubator
//
// bin/mail/incubator.go - import legacy .eml corpora into the docker-mailserver
// incubator mailbox (issue #252 / epic #250).
//
//	./bin/mail/incubator.go --dry-run              # scan + plan, no writes
//	./bin/mail/incubator.go --limit 1000           # pilot: first 1000 messages
//	./bin/mail/incubator.go --folders --limit 1000 # replicate the folder tree
//
// Reads the incubator.* section of the typed config (etc/brain/config.yml; the
// source corpus roots are machine-local inventory, see #79 — put them in
// config.local.yml). For every configured import (label = source owner, e.g.
// wheregroup → the owner's historical account andriy.oblivantsev@wheregroup.com)
// the tool walks the Thunderbird profile dump, derives the canonical
// Message-ID dedup key per message (fallback: body sha256 for id-less mail),
// skips already-imported messages via the manifest
// var/state/incubator-<label>.json (gitignored runtime state) and pushes new
// ones into the owner mailbox with
// `docker exec -i mailserver doveadm save` over stdin — no bind-mount of the
// legacy corpus into the container is needed. The incubator mailbox IS the
// owner's historical address (decision 2026-09-02, #252), so when an owner
// address is configured (import.owner / --owner) each message is routed by
// recipient: owner in From → Sent, owner in To/CC/Delivered-To → INBOX, owner
// nowhere → INBOX/Unmatched (quarantine for triage). Without an owner the tool
// keeps the legacy targets: flat INBOX (pilot default) or the replicated
// folder tree (--folders, issue #252 п.3).
// Re-runs are idempotent: the manifest is the source of truth (doveadm save
// itself does NOT dedup — verified live, 2026-09-02). Logic lives in
// internal/incubator (Run/Scan/MailboxOfDir/LayoutOf); this tool is a thin
// CLI wrapper.
//
// NOTE: never run gofmt -w — it rewrites the shebang.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/incubator"
	cliparse "github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/utils"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	var dry, folders, force bool
	var limit int
	var ownerFlag string
	p := cliparse.New("mail-incubator")
	p.Description = "legacy .eml corpus → doveadm save into incubator mailbox (#252)"
	p.Bool(&dry, "", "dry-run", "scan + plan only: no doveadm calls, no manifest write")
	p.Bool(&folders, "", "folders", "replicate the source folder tree (INBOX/<Folder>/...); default = flat INBOX")
	p.Bool(&force, "", "force", "ignore the manifest and re-import the window (only after wiping the mailbox — doveadm save does not dedup)")
	p.Int(&limit, "", "limit", "max messages per import run (0 = all; pilot = 1000)")
	p.String(&ownerFlag, "", "owner", "historical owner address: route by recipient (owner in From → Sent, To/CC/Delivered-To → INBOX, else INBOX/Unmatched); default = the import's config owner, unset = legacy flat/folder import")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail/incubator:", err)
		return 1
	}
	if len(cfg.Incubator.Imports) == 0 {
		fmt.Fprintln(os.Stderr, "mail/incubator: incubator.imports is empty — configure etc/brain/config.yml or config.local.yml (see #252/#79)")
		return 2
	}

	rc := 0
	for _, imp := range cfg.Incubator.Imports {
		owner := imp.Owner
		if ownerFlag != "" {
			owner = ownerFlag
		}
		o := incubator.Options{
			Root:      imp.Source,
			User:      imp.User,
			Owner:     owner,
			State:     impState(cfg, imp),
			Docker:    cfg.Incubator.Docker,
			Container: cfg.Incubator.Container,
			Limit:     limit,
			Folders:   folders,
			Force:     force,
			Dry:       dry,
		}
		st, err := incubator.Run(ctx, o)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mail/incubator: %s: %v\n", imp.Label, err)
			rc = 1
			continue
		}
		mode := "imported"
		if dry {
			mode = "dry-run"
		}
		line := fmt.Sprintf("mail/incubator: %s: found=%d unique=%d no-id=%d window=%d new=%d already=%d dup=%d (%s, user=%s)",
			imp.Label, st.Scanned, st.Unique, st.NoID, st.Window, st.Imported, st.Already, st.DupInRun, mode, imp.User)
		if o.Owner != "" {
			line += fmt.Sprintf(", layout-owner=%s", o.Owner)
		}
		fmt.Println(line)
		printMailboxMap(st)
		printTargets(st)
	}
	return rc
}

// printMailboxMap prints the per-folder distribution of the scan — the
// "карта папок" of issue #252 (source folders as doveadm would see them).
func printMailboxMap(st incubator.Stats) {
	if len(st.ByMailbox) == 0 {
		return
	}
	mbs := make([]string, 0, len(st.ByMailbox))
	for mb := range st.ByMailbox {
		mbs = append(mbs, mb)
	}
	sort.Strings(mbs)
	for _, mb := range mbs {
		fmt.Printf("    %-70s %d\n", mb, st.ByMailbox[mb])
	}
}

// printTargets prints the recipient-routing result of layout mode
// (Sent/INBOX/INBOX/Unmatched, decision 2026-09-02 / #252).
func printTargets(st incubator.Stats) {
	if len(st.Targets) == 0 {
		return
	}
	fmt.Println("    targets (layout):")
	mbs := make([]string, 0, len(st.Targets))
	for mb := range st.Targets {
		mbs = append(mbs, mb)
	}
	sort.Strings(mbs)
	for _, mb := range mbs {
		fmt.Printf("    %-70s %d\n", mb, st.Targets[mb])
	}
}

func impState(cfg *config.Config, imp config.IncubatorImport) string {
	return utils.Or(imp.State, filepath.Join(cfg.Root, "var", "state", "incubator-"+imp.Label+".json"))
}
