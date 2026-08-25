// Package cli is the shared flaggy wrapper (D23).
//
// flaggy: zero deps, flags at any position, shell completion scripts.
// Individual tools keep ShowCompletion off so a query like "completion" is
// not stolen; dump scripts with bin/shell/complete.go.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/integrii/flaggy"
)

// ErrHelp means -h/--help was requested (exit 0).
var ErrHelp = errors.New("help")

// ExitSkip is the exit code a tool returns when its work is deliberately not
// run — credentials or a source session are missing (e.g. chats without
// TELEGRAM_* / LINKEDIN_* config). The stack wave prints SKIP for it and does
// not count it as a failure.
const ExitSkip = 3

var parseMu sync.Mutex

// New returns a per-call parser. Never reuse: flaggy parses once.
func New(name string) *flaggy.Parser {
	p := flaggy.NewParser(name)
	p.ShowVersionWithVersionFlag = false
	p.ShowCompletion = false
	// Extra positionals become TrailingArguments (search "two words --json").
	// Unknown dash tokens are rejected in Parse after flaggy returns.
	p.ShowHelpOnUnexpected = false
	p.ShowHelpWithHFlag = true
	return p
}

// Parse runs p.ParseArgs and turns flaggy's os.Exit into an error.
// Not safe to call in parallel (flaggy.PanicInsteadOfExit is process-global).
func Parse(p *flaggy.Parser, args []string) error {
	parseMu.Lock()
	defer parseMu.Unlock()
	prev := flaggy.PanicInsteadOfExit
	flaggy.PanicInsteadOfExit = true
	defer func() { flaggy.PanicInsteadOfExit = prev }()

	var exitMsg string
	err := func() error {
		defer func() {
			if r := recover(); r != nil {
				exitMsg = fmt.Sprint(r)
			}
		}()
		return p.ParseArgs(args)
	}()
	if err != nil {
		return err
	}
	if exitMsg != "" {
		if strings.Contains(exitMsg, "code: 0") {
			return ErrHelp
		}
		return errors.New(exitMsg)
	}
	if u := unknownFlags(p, args); len(u) > 0 {
		return fmt.Errorf("unknown flag %q", u[0])
	}
	return nil
}

func unknownFlags(p *flaggy.Parser, args []string) []string {
	flags := collectFlags(&p.Subcommand)
	var out []string
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--" {
			break
		}
		name, inline := flagName(a)
		if name == "" {
			continue
		}
		if name == "h" || name == "help" {
			continue
		}
		f := findFlag(flags, name)
		if f == nil {
			out = append(out, a)
			continue
		}
		if !inline && !isBoolFlag(f) {
			skipNext = true
		}
	}
	return out
}

func flagName(a string) (name string, inline bool) {
	if a == "-" || !strings.HasPrefix(a, "-") {
		return "", false
	}
	rest := strings.TrimLeft(a, "-")
	name, _, inline = strings.Cut(rest, "=")
	return name, inline
}

func collectFlags(sc *flaggy.Subcommand) []*flaggy.Flag {
	out := append([]*flaggy.Flag{}, sc.Flags...)
	for _, sub := range sc.Subcommands {
		out = append(out, collectFlags(sub)...)
	}
	return out
}

func findFlag(flags []*flaggy.Flag, name string) *flaggy.Flag {
	for _, f := range flags {
		if f.HasName(name) {
			return f
		}
	}
	return nil
}

func isBoolFlag(f *flaggy.Flag) bool {
	_, ok := f.AssignmentVar.(*bool)
	return ok
}

// Query joins the first positional with leftover trailing words.
func Query(first string, trailing []string) string {
	parts := make([]string, 0, 1+len(trailing))
	if s := strings.TrimSpace(first); s != "" {
		parts = append(parts, s)
	}
	for _, t := range trailing {
		if s := strings.TrimSpace(t); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// Code maps parse errors to process exit codes (0 help, 2 usage).
func Code(err error) int {
	if err == nil || errors.Is(err, ErrHelp) {
		return 0
	}
	return 2
}

// Fail prints err unless it is help or a flaggy exit that already wrote stderr.
func Fail(err error) int {
	if err == nil || errors.Is(err, ErrHelp) {
		return 0
	}
	if strings.HasPrefix(err.Error(), "Panic instead of exit") {
		return 2
	}
	fmt.Fprintln(os.Stderr, err)
	return 2
}

// Tool is one shebang CLI for completion dump.
type Tool struct {
	Path string
	Name string
	New  func() *flaggy.Parser
}

// BashScript concatenates flaggy bash complete scripts and binds each
// function to the shebang path (./bin/subject/method.go).
func BashScript(tools []Tool) string {
	var b strings.Builder
	b.WriteString("# 2dph flaggy completions (D23). source <(./bin/shell/complete.go bash)\n")
	for _, t := range tools {
		p := t.New()
		p.Name = t.Name
		script := flaggy.GenerateBashCompletion(p)
		b.WriteString(script)
		fn := "_" + strings.ReplaceAll(t.Name, "-", "_") + "_complete"
		if t.Path != "" && t.Path != t.Name {
			fmt.Fprintf(&b, "complete -F %s %s\n", fn, t.Path)
			if !strings.HasPrefix(t.Path, "./") {
				fmt.Fprintf(&b, "complete -F %s ./%s\n", fn, t.Path)
			}
		}
	}
	return b.String()
}
