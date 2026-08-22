//usr/bin/env go run -tags=chats_facts "$0" "$@"; exit
//go:build chats_facts
//
// bin/chat/facts.go - extract phone/email/linkedin facts from JSONL.
//
//	./bin/chat/facts.go
//
// Writes var/corpus/chats/facts/. Does not index the brain.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/chat"
)

func main() {
	os.Exit(chat.RunFacts(os.Args[1:]))
}
