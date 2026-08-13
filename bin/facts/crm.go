//usr/bin/env go run -tags=facts_crm "$0" "$@"; exit
//go:build facts_crm
//
// bin/facts/crm.go - prove person↔company / company↔project (ooCRM × corpus).
//
//	./bin/facts/crm.go [--dry-run] [--mismatches]
//
// Python bin/facts/crm is the implementation. Graph write stays Python.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/facts/crm", os.Args[1:]))
}
