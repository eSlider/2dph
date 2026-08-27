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
	"context"
	"log"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/pkg/httpapi"
)

func main() {
	cfg, err := config.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	brain.Configure(cfg)
	if err := brain.Ready(); err != nil {
		log.Fatal(err)
	}
	// Warm start (#206): load the ANN index at startup (no rebuild) so the
	// first query is already fast; missing/corrupt index → fallback to the
	// linear scan until the wave's ann-build step builds it.
	brain.WarmANN()
	httpapi.Run(brain.HTTP{}, cfg)
}
