//usr/bin/env go run "$0" "$@"; exit
//
// bin/mail/watch - IMAP sync → markdown → live brain ingest loop.
//
//	./bin/mail/watch --source imap --env .secrets/mail.env
//	./bin/mail/watch --source imap --once
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/mailsync"
)

func main() {
	os.Exit(mailsync.WatchMain(os.Args[1:]))
}
