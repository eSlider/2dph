//usr/bin/env go run -tags=chats_facts "$0" "$@"; exit
//go:build chats_facts
//
// bin/chats/facts.go - extract phone/email/linkedin facts from JSONL.
//
//	./bin/chats/facts.go
//
// Writes var/chats/facts/. Does not index the brain.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/chats"
)

func main() {
	os.Exit(chats.RunFacts(os.Args[1:]))
}
