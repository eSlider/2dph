//usr/bin/env go run -tags=mail_import_pst "$0" "$@"; exit
//go:build mail_import_pst
//
// bin/mail/import-pst.go - Outlook PST → .eml → mail pipeline (#185).
//
//	./bin/mail/import-pst.go --dry-run
//	./bin/mail/import-pst.go
//
// Reads the pst.* section of the typed config (etc/brain/config.yml; the
// source .pst inventory is machine-local, see #79 — put the paths in
// config.local.yml). For each configured source, readpst -e extracts the
// archive into a wiped scratch dir (var/tmp/pst), every extracted .eml is
// copied content-addressed into var/corpus/mail/pst/<label>/<folder>/<id>/<id>.eml
// (same layout as the mbox splitter) and the tree is converted through the
// shared mailconv.FromEML path (source tag pst/). Re-runs are idempotent: the
// source.Sync seen-set (var/state/pst.json) plus the content-addressed layout
// guarantee zero duplicates. Orchestration lives in internal/source
// (ImportPST/PlanPST); this tool is a thin CLI wrapper.
//
// NOTE: never run gofmt -w — it rewrites the shebang.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	cliparse "github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/source"
	"github.com/eSlider/2dph/pkg/utils"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	var dry bool
	p := cliparse.New("mail-import-pst")
	p.Description = "Outlook PST → .eml → mail pipeline (#185)"
	p.Bool(&dry, "", "dry-run", "print the plan without running readpst or writing")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail/import-pst:", err)
		return 1
	}
	if len(cfg.PST.Sources) == 0 {
		fmt.Fprintln(os.Stderr, "mail/import-pst: pst.sources is empty — configure etc/brain/config.yml (see #79)")
		return 2
	}
	o := source.ImportOptions{
		Sources:   pstSources(cfg.PST.Sources),
		Staging:   pstScratch(cfg),
		Out:       pstOut(cfg),
		ReadPST:   cfg.PST.ReadPST,
		StatePath: pstState(cfg),
	}
	if dry {
		for _, l := range source.PlanPST(o) {
			fmt.Println("  " + l)
		}
		return 0
	}

	st, conv, err := source.ImportPST(ctx, o)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail/import-pst:", err)
		return 1
	}
	fmt.Printf("mail/import-pst: extracted=%d new=%d skipped=%d converted ok=%d skip=%d fail=%d (state %s)\n",
		st.Fetched, st.New, st.Skipped, conv.OK, conv.Skip, conv.Fail, o.StatePath)
	if conv.Fail > 0 {
		return 1
	}
	return 0
}

// pstSources maps the config sources onto the adapter type.
func pstSources(in []config.PSTSource) []source.PSTSource {
	out := make([]source.PSTSource, 0, len(in))
	for _, s := range in {
		out = append(out, source.PSTSource{Label: s.Label, Path: s.Path})
	}
	return out
}

func pstOut(cfg *config.Config) string {
	return utils.Or(cfg.PST.Out, filepath.Join(cfg.Root, "var", "corpus", "mail", "pst"))
}

func pstScratch(cfg *config.Config) string {
	return filepath.Join(cfg.Root, "var", "tmp", "pst")
}

func pstState(cfg *config.Config) string {
	return utils.Or(cfg.PST.State, filepath.Join(cfg.Root, "var", "state", "pst.json"))
}
