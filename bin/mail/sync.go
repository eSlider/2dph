//usr/bin/env go run "$0" "$@"; exit
// bin/mail/sync.go - async download of OnlyOffice, Gmail and M365 mail to var/mail/.
//
//	./bin/mail/sync.go --source onlyoffice,gmail,m365 --limit 50 --workers 8
//	./bin/mail/sync.go --source gmail --force
//	./bin/mail/sync.go --source m365 --env .secrets/m365.env
//	./bin/mail/sync.go --dry-run
//
// Writes raw message.json + attachments under var/mail/<folder>/<id>/; run
// bin/mail/import.go --from-raw afterwards to convert everything to markdown.
//
// Shebang trick: first line is a Go `//` comment; the real code lives in the
// importable package (module path, never a relative import).
// NOTE: never run `gofmt -w` on this file - it rewrites `//usr/bin/env` to
// `// usr/...` and breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/bin/mail/sync"
)

func main() {
	os.Exit(sync.Main(os.Args[1:]))
}
