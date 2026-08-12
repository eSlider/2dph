// bin/kbsearch - the Go implementation of bin/kb/search (nested module so the
// root `go test ./...` and CI never compile it against native ladyships).
//
// Usage (built/run by ./bin/kb/search):
//
//	kbsearch "query" [--root facts|info] [--repo P] [-n N] [--json]
//	kbsearch serve [port]           start the embedding daemon
//	kbsearch --list-model           print the resolved model dir
//
// The potion-multilingual model is loaded only in `serve`; a CLI reuses the
// daemon over localhost HTTP (KBSEARCH_PORT, default 17830) and starts one in
// the background when none answers. KBSEARCH_NO_DAEMON=1 skips that and embeds
// in-process instead, which costs a full model load per query.
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		port := 17830
		if len(os.Args) > 2 {
			if p, err := strconv.Atoi(os.Args[2]); err == nil {
				port = p
			}
		}
		if err := serve(port); err != nil {
			log.Fatalf("kbsearch serve: %v", err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--list-model" {
		dir, err := modelDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(dir)
		return
	}
	os.Exit(runSearch(os.Args[1:]))
}