//usr/bin/env go run "$0" "$@"; exit
// bin/serve.go - async Go HTTP server for the 2dph brain (see bin/server).
//
//	KB_ROOT=/path/to/2dph ./bin/serve.go        # serve the brain
//	KB_SEARCH_CMD=... KB_WORKERS=4 KB_PORT=8630 ./bin/serve.go
//
// Shebang trick: the first line is a Go `//` comment; when executed, env runs
// `go run "$0"` so this file doubles as an executable script. The real code
// lives in the importable package (module path, never a relative import).
// NOTE: never run `gofmt -w` on this file - it rewrites `//usr/bin/env` to
// `// usr/...` and breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/bin/server"
)

func main() {
	if env := os.Getenv("KB_ROOT"); env == "" {
		if wd, err := os.Getwd(); err == nil {
			os.Setenv("KB_ROOT", wd)
		}
	}
	server.Run()
}
