//usr/bin/env go run -tags=chats_sync "$0" "$@"; exit
//go:build chats_sync
//
// bin/chat/sync.go - platform sync entrypoints (telegram|linkedin|whatsapp)
// plus the jsonl→md importer. Downloads chat messages to
// var/corpus/chats/<platform>/.
//
//	./bin/chat/sync.go telegram [--limit N] [--phone PHONE]
//	./bin/chat/sync.go linkedin [--limit N] [--refresh]
//	./bin/chat/sync.go whatsapp --from DIR [--out DIR]
//	./bin/chat/sync.go import [platform]
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"

	"github.com/eSlider/2dph/internal/chat"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, `usage: bin/chat/sync.go telegram|linkedin|whatsapp [flags]`)
		os.Exit(2)
	}
	platform := os.Args[1]
	args := os.Args[2:]
	switch platform {
	case "telegram":
		os.Exit(chat.RunSyncTelegram(args))
	case "linkedin":
		os.Exit(chat.RunSyncLinkedIn(args))
	case "whatsapp":
		os.Exit(chat.RunSyncWhatsApp(args))
	case "import":
		os.Exit(chat.RunImport(args))
	default:
		fmt.Fprintf(os.Stderr, "chats: unknown platform %q\n", platform)
		os.Exit(2)
	}
}
