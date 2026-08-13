//usr/bin/env go run -tags=brain_eval "$0" "$@"; exit
//go:build brain_eval
//
// bin/brain/eval.go - recall@5 gate.
//
//	./bin/brain/eval.go
//	./bin/brain/eval.go --json
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/kb/eval", os.Args[1:]))
}
