package chat

import (
	"os"
	"strings"
)

// Root locates the 2dph project root (KB_ROOT, or walk up for var/ or .git).
func Root() string {
	if v := os.Getenv("KB_ROOT"); v != "" {
		return v
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(wd + "/var"); err == nil {
			return wd
		}
		if _, err := os.Stat(wd + "/.git"); err == nil {
			return wd
		}
		parent := wd
		if idx := strings.LastIndex(wd, "/"); idx >= 0 {
			parent = wd[:idx]
		}
		if parent == wd {
			break
		}
		wd = parent
	}
	return "."
}

// Dir is var/chats under the project root.
func Dir() string {
	return Root() + "/var/chats"
}
