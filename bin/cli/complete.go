//usr/bin/env go run "$0" "$@"; exit
//
// bin/cli/complete.go - dump flaggy shell completions for all Go shebang tools (D23).
//
//	source <(./bin/cli/complete.go bash)
//	./bin/cli/complete.go zsh|fish|powershell|nushell
//
// Search does not steal the word "completion"; this binary dumps scripts.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"
	"strings"

	mailsync "github.com/eSlider/2dph/bin/mail/sync"
	"github.com/eSlider/2dph/internal/brain/rank"
	"github.com/eSlider/2dph/internal/chats"
	"github.com/eSlider/2dph/internal/cli"
	"github.com/eSlider/2dph/internal/gitlog"
	"github.com/eSlider/2dph/internal/mdleaves"
	"github.com/eSlider/2dph/internal/ocr"
	"github.com/eSlider/2dph/internal/reasoner"
	"github.com/eSlider/2dph/internal/websearch"
	"github.com/integrii/flaggy"
)

func tools() []cli.Tool {
	return []cli.Tool{
		{Path: "bin/brain/search.go", Name: "brain-search", New: rank.Parser},
		{Path: "bin/brain/get.go", Name: "brain-get", New: func() *flaggy.Parser {
			o := rank.GetOptions{}
			return rank.GetParser(&o)
		}},
		{Path: "bin/brain/stats.go", Name: "brain-stats", New: rank.StatsParser},
		{Path: "bin/brain/eval.go", Name: "brain-eval", New: rank.EvalParser},
		{Path: "bin/web/search.go", Name: "web-search", New: websearch.Parser},
		{Path: "bin/git/import.go", Name: "git-import", New: gitlog.Parser},
		{Path: "bin/markdown/import.go", Name: "markdown-import", New: mdleaves.Parser},
		{Path: "bin/qa/stats.go", Name: "qa-stats", New: cli.QAParser},
		{Path: "bin/reasoner/bakeoff.go", Name: "reasoner-bakeoff", New: reasoner.Parser},
		{Path: "bin/mail/ocr.go", Name: "mail-ocr", New: ocr.Parser},
		{Path: "bin/mail/sync.go", Name: "mail-sync", New: mailsync.Parser},
		{Path: "bin/chats/sync.go", Name: "chats-sync", New: chats.SyncParser},
		{Path: "bin/chats/import.go", Name: "chats-import", New: chats.ImportParser},
		{Path: "bin/chats/facts.go", Name: "chats-facts", New: chats.FactsParser},
		{Path: "bin/chats/apply.go", Name: "chats-apply", New: chats.ApplyParser},
	}
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	shell := "bash"
	if len(args) > 0 {
		switch args[0] {
		case "bash", "zsh", "fish", "powershell", "nushell":
			shell = args[0]
		case "-h", "--help", "help":
			fmt.Fprintln(os.Stderr, "usage: bin/cli/complete.go [bash|zsh|fish|powershell|nushell]")
			return 0
		default:
			fmt.Fprintf(os.Stderr, "cli/complete: unknown shell %q\n", args[0])
			return 2
		}
	}
	if shell == "bash" {
		fmt.Print(cli.BashScript(tools()))
		return 0
	}
	var b strings.Builder
	for _, t := range tools() {
		p := t.New()
		p.Name = t.Name
		switch shell {
		case "zsh":
			b.WriteString(flaggy.GenerateZshCompletion(p))
		case "fish":
			b.WriteString(flaggy.GenerateFishCompletion(p))
		case "powershell":
			b.WriteString(flaggy.GeneratePowerShellCompletion(p))
		case "nushell":
			b.WriteString(flaggy.GenerateNushellCompletion(p))
		}
	}
	fmt.Print(b.String())
	return 0
}
