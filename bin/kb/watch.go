//usr/bin/env go run "$0" "$@"; exit
// bin/kb/watch.go - re-index the 2dph brain when corpus files change.
//
// Usage:
//
//	./bin/kb/watch.go [dir...]            # dirs default /corpus
//	KB_WATCH_INTERVAL=15 ./bin/kb/watch.go
//
// Shebang trick: first line is a Go `//` comment; the real code lives in the
// importable package (module path, never a relative import).
// NOTE: never run `gofmt -w` on this file - it rewrites `//usr/bin/env` to
// `// usr/...` and breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/bin/watch"
)

func main() {
	watch.Run(os.Args[1:])
}
