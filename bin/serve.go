//usr/bin/env go run -tags=brain_serve "$0" "$@"; exit
//go:build brain_serve
//
// bin/serve.go — deprecated; use bin/brain/serve.go.
package main

import (
	"fmt"
	"os"

	"github.com/eSlider/2dph/internal/httpapi"
)

func main() {
	fmt.Fprintln(os.Stderr, "bin/serve.go is deprecated; use bin/brain/serve.go")
	if os.Getenv("KB_ROOT") == "" {
		if wd, err := os.Getwd(); err == nil {
			os.Setenv("KB_ROOT", wd)
		}
	}
	httpapi.Run()
}
