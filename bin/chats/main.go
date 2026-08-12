// bin/chats - sync, import, index, extract facts, and apply chat data
// from Telegram, WhatsApp, LinkedIn into the brain and OnlyOffice CRM.
//
// Usage:
//
//	chats sync telegram   [--limit N] [--since DATE] [--phone PHONE]
//	chats sync whatsapp   [--qr] [--limit N]
//	chats sync linkedin   [--limit N]
//	chats import                           # JSONL → MD (all sources)
//	chats index                            # rebuild var/kb.lbug with chats
//	chats facts                            # extract + cross-check
//	chats apply          [--dry-run]       # push to OnlyOffice CRM
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "sync":
		if len(args) < 1 {
			usage()
			os.Exit(2)
		}
		platform := args[0]
		platformArgs := args[1:]
		switch platform {
		case "telegram":
			os.Exit(runSyncTelegram(platformArgs))
		case "whatsapp":
			fmt.Fprintf(os.Stderr, "chats: WhatsApp not implemented yet\n")
			os.Exit(1)
		case "linkedin":
			os.Exit(runSyncLinkedIn(platformArgs))
		default:
			fmt.Fprintf(os.Stderr, "chats: unknown platform %q\n", platform)
			os.Exit(2)
		}
	case "import":
		os.Exit(runImport(args))
	case "index":
		os.Exit(runIndex(args))
	case "facts":
		os.Exit(runFacts(args))
	case "apply":
		os.Exit(runApply(args))
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "chats: unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	w := os.Stderr
	fmt.Fprintln(w, `Usage: chats <command> [args]

Commands:
  sync telegram  [--limit N] [--since DATE] [--phone PHONE]
  sync whatsapp  [--qr] [--limit N]
  sync linkedin  [--limit N]
  import                             JSONL → MD (all sources)
  index                              rebuild var/kb.lbug with chats
  facts                              extract + cross-check facts
  apply             [--dry-run]      push to OnlyOffice CRM

Output layout:
  var/chats/<platform>/<chat_id>/messages.jsonl
  var/chats/md/<platform>/<chat_name>/messages.md`)
}

// repoRoot locates the 2dph project root by walking up from the binary.
func repoRoot() string {
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

// chatsDir returns var/chats under the repo root.
func chatsDir() string {
	return repoRoot() + "/var/chats"
}
