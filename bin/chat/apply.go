//usr/bin/env go run -tags=chats_apply "$0" "$@"; exit
//go:build chats_apply
//
// bin/chat/apply.go - push extracted chat facts to OnlyOffice CRM.
//
//	./bin/chat/apply.go [--dry-run]
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/chat"
)

func main() {
	os.Exit(chat.RunApply(os.Args[1:]))
}
