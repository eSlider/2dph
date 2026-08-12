package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runIndex(args []string) int {
	fs := flag.NewFlagSet("chats index", flag.ContinueOnError)
	help := fs.Bool("help", false, "")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *help {
		fmt.Fprintln(os.Stderr, "usage: chats index")
		return 0
	}

	root := repoRoot()
	mdDir := filepath.Join(chatsDir(), "md")

	_, err := os.Stat(mdDir)
	if os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "chats index: no chat markdown at %s; run 'chats import' first\n", mdDir)
		return 1
	}

	indexScript := filepath.Join(root, "bin", "kb", "index")
	if _, err := os.Stat(indexScript); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "chats index: %s not found\n", indexScript)
		return 1
	}

	cmd := exec.Command(indexScript, "--corpus", mdDir)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Dir = root

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "chats index: %v\n%s", err, errBuf.String())
		return 1
	}
	result := strings.TrimSpace(outBuf.String())
	if result == "" {
		result = strings.TrimSpace(errBuf.String())
	}
	fmt.Printf("chats index: %s\n", result)
	return 0
}
