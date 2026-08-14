//usr/bin/env go run -tags=chats_sync "$0" "$@"; exit
//go:build chats_sync
//
// bin/chats/sync.go - download chat messages to var/chats/<platform>/.
//
//	./bin/chats/sync.go telegram [--limit N] [--phone PHONE]
//	./bin/chats/sync.go linkedin [--limit N] [--refresh]
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"

	"github.com/eSlider/2dph/internal/chats"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, `usage: bin/chats/sync.go telegram|linkedin [flags]`)
		os.Exit(2)
	}
	platform := os.Args[1]
	args := os.Args[2:]
	switch platform {
	case "telegram":
		os.Exit(chats.RunSyncTelegram(args))
	case "linkedin":
		os.Exit(chats.RunSyncLinkedIn(args))
	case "whatsapp":
		fmt.Fprintln(os.Stderr, "chats: WhatsApp sync is out of v1")
		os.Exit(1)
	case "help", "-h", "--help":
		fmt.Fprintln(os.Stderr, `usage: bin/chats/sync.go telegram|linkedin [flags]
WhatsApp sync is out of v1.`)
		return
	default:
		fmt.Fprintf(os.Stderr, "chats: unknown platform %q\n", platform)
		os.Exit(2)
	}
}
