//usr/bin/env go run -tags=chats_apply "$0" "$@"; exit
//go:build chats_apply
//
// bin/chats/apply.go - push extracted chat facts to OnlyOffice CRM.
//
//	./bin/chats/apply.go [--dry-run]
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/chats"
)

func main() {
	os.Exit(chats.RunApply(os.Args[1:]))
}
