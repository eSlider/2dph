//go:build brain_serve && !system_ladybug
//
// Fallback serve when ladybug cgo is not in the build (CI / tags=brain_serve).
// Production shebang is serve.go (in-process).
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
	httpapi.Run(nil)
}
