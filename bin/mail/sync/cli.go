// Package synccmd wires the sync library to a CLI: reads .env, parses flags,
// picks sources, prints stats. Kept separate from the library so unit tests
// don't depend on os.Args/env.
package sync

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CLIConfig is a superset of SyncConfig plus flag parsing results.
type CLIConfig struct {
	Sync SyncConfig
	Env  string // .env path; default <cwd>/.env
	Sources string
	Help bool
}

// ParseCLI reads os.Args into a CLIConfig. Exit codes: 0 ok, 2 usage.
func ParseCLI(args []string) (CLIConfig, int, error) {
	fs := flag.NewFlagSet("mail/sync", flag.ContinueOnError)
	var (
		env    = fs.String("env", "", ".env file (default: <cwd>/.env)")
		out    = fs.String("out", "", "var/mail root (default: <cwd>/var/mail)")
		workers = fs.Int("workers", 4, "concurrent downloads")
		limit  = fs.Int("limit", 0, "max messages per source (0 = all)")
		offset = fs.Int("offset", 0, "skip first N messages per source")
		force  = fs.Bool("force", false, "overwrite existing message.json + attachments")
		dryRun = fs.Bool("dry-run", false, "list message counts without writing")
		srcs   = fs.String("source", "onlyoffice", "comma list: onlyoffice,gmail (default onlyoffice)")
		help   = fs.Bool("help", false, "usage")
	)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return CLIConfig{}, 2, err
	}
	if *help || fs.NArg() > 0 {
		return CLIConfig{Help: true}, 0, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return CLIConfig{}, 2, err
	}
	if *env == "" {
		*env = filepath.Join(wd, ".env")
	}
	if *out == "" {
		*out = filepath.Join(wd, "var", "mail")
	}
	envVars := readEnv(*env)
	cfg := SyncConfig{
		Out:     *out,
		Workers: *workers,
		Limit:   *limit,
		Offset:  *offset,
		Force:   *force,
		DryRun:  *dryRun,
		Policy:  RetryPolicy{},
	}
	cli := CLIConfig{Sync: cfg, Env: *env, Sources: *srcs}
	for _, s := range strings.Split(*srcs, ",") {
		switch strings.TrimSpace(s) {
		case "onlyoffice":
			u := pick(envVars["ONLYOFFICE_URL"], envVars["OO_URL"])
			user := pick(envVars["ONLYOFFICE_USER"], envVars["OO_USER"])
			pass := pick(envVars["ONLYOFFICE_PASS"], envVars["OO_PASSWORD"])
			if u == "" || user == "" || pass == "" {
				return CLIConfig{}, 2, fmt.Errorf("onlyoffice source needs ONLYOFFICE_URL/USER/PASS in %s", *env)
			}
			cfg.OO = &OOConfig{URL: u, User: user, Password: pass}
		case "gmail":
			home, _ := os.UserHomeDir()
			cfg.Gmail = &GmailCredentials{
				CredentialsPath: filepath.Join(home, ".gmail-mcp", "credentials.json"),
				KeysPath:        filepath.Join(home, ".gmail-mcp", "gcp-oauth.keys.json"),
			}
		default:
			return CLIConfig{}, 2, fmt.Errorf("unknown source %q", s)
		}
	}
	cli.Sync = cfg
	return cli, 0, nil
}

// Main is the CLI entry: returns process exit code.
func Main(args []string) int {
	cli, code, err := ParseCLI(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail/sync:", err)
		return code
	}
	if cli.Help {
		fmt.Fprintln(os.Stderr, "usage: bin/mail/sync.go [--source onlyoffice,gmail] [--limit N] [--offset N] [--workers N] [--force] [--dry-run]")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	start := time.Now()
	stats, err := Run(ctx, cli.Sync)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail/sync:", err)
		return 1
	}
	if cli.Sync.DryRun {
		fmt.Printf("mail/sync: dry-run checked=%d (no writes)\n", stats.Checked)
		return 0
	}
	fmt.Printf("mail/sync: checked=%d new=%d skipped=%d failed=%d in %s\n",
		stats.Checked, stats.New, stats.Skipped, stats.Failed, time.Since(start).Round(time.Millisecond))
	if stats.Failed > 0 {
		return 1
	}
	return 0
}

// readEnv parses KEY=VALUE lines (ignoring comments) with KEY=PATH override.
func readEnv(path string) map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), "\"'")
	}
	// env overrides file
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(k, "ONLYOFFICE_") || strings.HasPrefix(k, "OO_") {
			out[k] = v
		}
	}
	return out
}

func pick(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
