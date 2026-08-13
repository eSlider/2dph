//usr/bin/env go run -tags=mail_import "$0" "$@"; exit
//go:build mail_import
//
// bin/mail/import.go - message.json → markdown (no brain write).
//
//	./bin/mail/import.go --from-raw var/mail
//
// Indexing is bin/brain/index.go --rebuild, not this command.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/mail/import", os.Args[1:]))
}
