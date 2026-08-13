//usr/bin/env go run -tags=brain_watch "$0" "$@"; exit
//go:build brain_watch
//
// bin/brain/watch.go - re-index when corpus files change.
//
//	./bin/brain/watch.go [dir...]
//	KB_WATCH_INTERVAL=15 ./bin/brain/watch.go
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"os"

	"github.com/eSlider/2dph/bin/watch"
)

func main() {
	watch.Run(os.Args[1:])
}
