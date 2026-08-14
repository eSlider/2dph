//usr/bin/env go run -tags=facts_audit "$0" "$@"; exit
//go:build facts_audit
//
// bin/facts/audit.go - 2-source + lexicon checks.
//
//	./bin/facts/audit.go self
//	./bin/facts/audit.go db
//	./bin/facts/audit.go contradict --json < claim.json
//
// Python bin/facts/audit is the implementation (CI runs it directly).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/cmdbin"
)

func main() {
	os.Exit(cmdbin.ExecFile("bin/facts/audit", os.Args[1:]))
}
