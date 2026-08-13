//usr/bin/env go run -tags=brain_serve "$0" "$@"; exit
//go:build brain_serve
//
// bin/brain/serve.go - HTTP API for the 2dph brain.
//
//	KB_ROOT=/path/to/2dph ./bin/brain/serve.go
//	KB_SEARCH_CMD=... KB_WORKERS=4 KB_PORT=8630 ./bin/brain/serve.go
//
// Default search backend is var/bin/brain-search (Go), not Python.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/internal/httpapi"
)

func main() {
	if os.Getenv("KB_ROOT") == "" {
		if wd, err := os.Getwd(); err == nil {
			os.Setenv("KB_ROOT", wd)
		}
	}
	httpapi.Run()
}
