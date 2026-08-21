//usr/bin/env go run -tags=chats_import "$0" "$@"; exit
//go:build chats_import
//
// bin/chat/import.go - JSONL → markdown under var/chats/md/.
//
//	./bin/chat/import.go
//
// Conversion only. Brain ingest is bin/brain/index.go, not this command.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/chat"
)

func main() {
	os.Exit(chat.RunImport(os.Args[1:]))
}
