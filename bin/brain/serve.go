//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=brain_serve,system_ladybug "$0" "$@"' "$0" "$@"; exit
//go:build brain_serve && cgo && system_ladybug
//
// bin/brain/serve.go - HTTP API (in-process ladybug search).
//
//	KB_ROOT=/path/to/2dph ./bin/brain/serve.go
//	KB_WORKERS=4 KB_PORT=8630 ./bin/brain/serve.go
//
//	GET  /openapi.json  same Ops table as the handlers
//	POST /mcp           JSON-RPC tools/list + tools/call
//
// Shebang routes through bin/cgo/zig (same as bin/brain/search.go).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"log"
	"os"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/pkg/httpapi"
)

func main() {
	if os.Getenv("KB_ROOT") == "" {
		if wd, err := os.Getwd(); err == nil {
			os.Setenv("KB_ROOT", wd)
		}
	}
	if err := brain.Ready(); err != nil {
		log.Fatal(err)
	}
	httpapi.Run(brain.HTTP{})
}
