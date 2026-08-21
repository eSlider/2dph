//usr/bin/env go run -tags=mail_import "$0" "$@"; exit
//go:build mail_import
//
// bin/mail/import.go - message.json → markdown (no brain write). Go converter.
//
//	./bin/mail/import.go --from-raw var/mail
//	./bin/mail/import.go --from-raw var/mail --ocr
//
// Live OnlyOffice pull is removed; use bin/mail/sync.go then --from-raw.
// Indexing is bin/brain/index.go --rebuild.
// NOTE: never run gofmt -w — it breaks the shebang.
package main

import (
	"fmt"
	"os"

	cliparse "github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/internal/mailconv"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var fromRaw, fromEML string
	var ocr, force, dryRun bool
	p := cliparse.New("mail-import")
	p.Description = "convert synced message.json or raw .eml to markdown"
	p.String(&fromRaw, "", "from-raw", "root with */*/message.json")
	p.String(&fromEML, "", "from-eml", "root with raw .eml files")
	p.Bool(&ocr, "", "ocr", "OCR images (PDFs OCR when textless)")
	p.Bool(&force, "", "force", "overwrite existing message.md")
	p.Bool(&dryRun, "", "dry-run", "list without writing")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	if fromRaw == "" && fromEML == "" {
		fmt.Fprintln(os.Stderr, "mail/import: --from-raw or --from-eml DIR required (live OO pull removed; use mail/sync.go)")
		return 2
	}
	var ok, skip, fail int
	var err error
	if fromEML != "" {
		ok, skip, fail, err = mailconv.FromEML(fromEML, ocr, force, dryRun)
	} else {
		ok, skip, fail, err = mailconv.FromRaw(fromRaw, ocr, force, dryRun)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mail/import: %v\n", err)
		return 1
	}
	fmt.Printf("mail/import: ok=%d skip=%d fail=%d\n", ok, skip, fail)
	if fail > 0 {
		return 1
	}
	return 0
}
