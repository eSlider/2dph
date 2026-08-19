//usr/bin/env go run "$0" "$@"; exit
//
// bin/markdown/import.go - split markdown into leafs (H2 boundaries).
//
//	./bin/markdown/import.go [dir]
//	./bin/markdown/import.go --files a.md,b.md --json
//
// Conversion only. Brain write is bin/brain/index.go.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"
	"strings"

	cliparse "github.com/eSlider/2dph/internal/cli"
	"github.com/eSlider/2dph/internal/mdleaves"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	c, err := mdleaves.ParseArgs(args)
	if err != nil {
		return cliparse.Fail(err)
	}
	jsonOut := c.JSONOut
	files := c.Files
	root := c.Root

	var paths []string
	if files != "" {
		for _, f := range strings.Split(files, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				paths = append(paths, f)
			}
		}
	} else {
		st, err := os.Stat(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "md/import: no such path %s\n", root)
			return 2
		}
		if !st.IsDir() {
			paths = []string{root}
		} else {
			var err error
			paths, err = mdleaves.WalkMarkdown(root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "md/import: %v\n", err)
				return 1
			}
		}
	}
	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "md/import: no markdown files")
		return 1
	}

	var all []mdleaves.Leaf
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "md/import: %s: %v\n", p, err)
			continue
		}
		all = append(all, mdleaves.ToAll(string(raw), p, "")...)
	}
	if jsonOut {
		s, err := mdleaves.EncodeJSON(all)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Print(s)
		return 0
	}
	fmt.Print(mdleaves.EncodeYAML(all))
	return 0
}
