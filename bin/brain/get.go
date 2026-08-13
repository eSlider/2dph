//usr/bin/env go run -tags=brain_get "$0" "$@"; exit
//go:build brain_get
//
// bin/brain/get.go - read one leaf by id.
//
//	./bin/brain/get.go <id>
//	./bin/brain/get.go <id> --body
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/kb/get", os.Args[1:]))
}
