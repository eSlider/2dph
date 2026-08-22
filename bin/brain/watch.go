//usr/bin/env go run -tags=brain_watch "$0" "$@"; exit
//go:build brain_watch
//
// bin/brain/watch.go - re-index when corpus files change.
//
//	./bin/brain/watch.go [dir...]
//	KB_WATCH_INTERVAL=15 ./bin/brain/watch.go   # via typed config (legacy KB_*)
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/corpuswatch"
)

func main() {
	cfg, err := config.Load(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	opts := corpuswatch.Options{
		Interval: time.Duration(cfg.WatchInterval) * time.Second,
		Dirs:     cfg.WatchDirs,
		IndexCmd: "<root>/bin/brain/index.go --with-mail",
		Root:     cfg.Root,
	}
	corpuswatch.Run(os.Args[1:], opts)
}
