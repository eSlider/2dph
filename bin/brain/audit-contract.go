//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_audit "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_audit
//
// bin/brain/audit-contract.go - read-only аудит leafs на соответствие контракту
// записи (P-9.2, docs/brain/contract.md).
//
//	./bin/brain/audit-contract.go
//	./bin/brain/audit-contract.go --json
//
// Shebang routes through bin/cgo/zig (Ladybug CGO via Zig).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/brain"
)

func main() {
	os.Exit(brain.MainAuditContract(os.Args[1:]))
}
