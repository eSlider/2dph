// Package synccmd wires the sync library to a CLI: reads .env, parses flags,
// picks sources, prints stats. Kept separate from the library so unit tests
// don't depend on os.Args/env.
package mailsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cliparse "github.com/eSlider/2dph/pkg/cli"
	"github.com/integrii/flaggy"
)

// CLIConfig is a superset of SyncConfig plus flag parsing results.
type CLIConfig struct {
	Sync    SyncConfig
	Env     string // .env path; default <cwd>/.env
	Sources string
	Help    bool
	// watch-loop extras (harmless for plain sync)
	Once  bool   // one cycle then exit
	Brain string // brain base URL for ingest push; default http://127.0.0.1:8630
	OCR   bool   // OCR attachments during conversion
	Local bool   // skip IMAP sync; only convert + push the local tree
}

type flagVals struct {
	env, out, srcs, query, brain string
	workers, limit, offset       int
	force, dryRun, raw           bool
	once, ocr, local             bool
}

func Parser() *flaggy.Parser {
	v := flagVals{workers: 4, query: "in:inbox", srcs: "onlyoffice"}
	return bind(&v)
}

func bind(v *flagVals) *flaggy.Parser {
	if v.workers == 0 {
		v.workers = 4
	}
	if v.query == "" {
		v.query = "in:inbox"
	}
	if v.srcs == "" {
		v.srcs = "onlyoffice"
	}
	p := cliparse.New("mail-sync")
	p.Description = "download mail to var/corpus/mail"
	p.String(&v.env, "", "env", ".env file")
	p.String(&v.out, "", "out", "var/corpus/mail root")
	p.Int(&v.workers, "", "workers", "concurrent downloads")
	p.Int(&v.limit, "", "limit", "max messages per source (0 = all)")
	p.Int(&v.offset, "", "offset", "skip first N messages per source")
	p.Bool(&v.force, "", "force", "overwrite existing message.json")
	p.Bool(&v.dryRun, "", "dry-run", "list counts without writing")
	p.Bool(&v.raw, "", "raw", "fetch raw RFC 822 (.eml) for Gmail instead of message.json")
	p.String(&v.query, "", "query", "Gmail search query")
	p.String(&v.srcs, "", "source", "comma list: onlyoffice,gmail,m365,imap")
	p.Bool(&v.once, "", "once", "watch: single cycle then exit")
	p.String(&v.brain, "", "brain", "watch: brain base URL (default http://127.0.0.1:8630)")
	p.Bool(&v.ocr, "", "ocr", "watch: OCR attachments during conversion")
	p.Bool(&v.local, "", "local", "skip IMAP sync; convert + push local tree only")
	return p
}

// ParseCLI reads args into a CLIConfig. Exit codes: 0 ok, 2 usage.
func ParseCLI(args []string) (CLIConfig, int, error) {
	v := flagVals{workers: 4, query: "in:inbox", srcs: "onlyoffice"}
	p := bind(&v)
	if err := cliparse.Parse(p, args); err != nil {
		if errors.Is(err, cliparse.ErrHelp) {
			return CLIConfig{Help: true}, 0, nil
		}
		return CLIConfig{}, 2, err
	}
	if len(p.TrailingArguments) > 0 {
		return CLIConfig{Help: true}, 0, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return CLIConfig{}, 2, err
	}
	if v.env == "" {
		v.env = filepath.Join(wd, ".env")
	}
	if v.out == "" {
		v.out = filepath.Join(wd, "var", "corpus", "mail")
	}
	envVars := readEnv(v.env)
	cfg := SyncConfig{
		Out:     v.out,
		Workers: v.workers,
		Limit:   v.limit,
		Offset:  v.offset,
		Force:   v.force,
		DryRun:  v.dryRun,
		Raw:     v.raw,
		Query:   v.query,
		Policy:  RetryPolicy{},
	}
	out := CLIConfig{Sync: cfg, Env: v.env, Sources: v.srcs, Once: v.once, Brain: v.brain, OCR: v.ocr, Local: v.local}
	for _, s := range strings.Split(v.srcs, ",") {
		switch strings.TrimSpace(s) {
		case "onlyoffice":
			u := pick(envVars["ONLYOFFICE_URL"], envVars["OO_URL"])
			user := pick(envVars["ONLYOFFICE_USER"], envVars["OO_USER"])
			pass := pick(envVars["ONLYOFFICE_PASS"], envVars["OO_PASSWORD"])
			if u == "" || user == "" || pass == "" {
				return CLIConfig{}, 2, fmt.Errorf("onlyoffice source needs ONLYOFFICE_URL/USER/PASS in %s", v.env)
			}
			cfg.OO = &OOConfig{URL: u, User: user, Password: pass}
		case "gmail":
			home, _ := os.UserHomeDir()
			cfg.Gmail = &GmailCredentials{
				CredentialsPath: filepath.Join(home, ".gmail-mcp", "credentials.json"),
				KeysPath:        filepath.Join(home, ".gmail-mcp", "gcp-oauth.keys.json"),
			}
		case "m365":
			tenant := pick(envVars["M365_TENANT"], envVars["MS_TENANT"])
			cid := pick(envVars["M365_CLIENT_ID"], envVars["MS_CLIENT_ID"])
			sec := pick(envVars["M365_CLIENT_SECRET"], envVars["MS_CLIENT_SECRET"])
			users := pick(envVars["M365_USERS"], envVars["MS_USERS"])
			if tenant == "" || cid == "" || sec == "" || users == "" {
				return CLIConfig{}, 2, fmt.Errorf("m365 source needs M365_TENANT/CLIENT_ID/CLIENT_SECRET/USERS in %s", v.env)
			}
			var userList []string
			for _, u := range strings.Split(users, ",") {
				if u = strings.TrimSpace(u); u != "" {
					userList = append(userList, u)
				}
			}
			if len(userList) == 0 {
				return CLIConfig{}, 2, fmt.Errorf("m365 source: M365_USERS empty")
			}
			var excl []string
			for _, f := range strings.Split(pick(envVars["M365_EXCLUDE_FOLDERS"], envVars["MS_EXCLUDE_FOLDERS"]), ",") {
				if f = strings.TrimSpace(f); f != "" {
					excl = append(excl, f)
				}
			}
			cfg.M365 = &M365Credentials{Tenant: tenant, ClientID: cid, ClientSecret: sec, Users: userList, ExcludeFolders: excl}
		case "imap":
			icfg, err := IMAPEnv(envVars)
			if err != nil {
				return CLIConfig{}, 2, fmt.Errorf("imap source: %w (in %s)", err, v.env)
			}
			cfg.IMAP = icfg
		default:
			return CLIConfig{}, 2, fmt.Errorf("unknown source %q", s)
		}
	}
	out.Sync = cfg
	return out, 0, nil
}

// Main is the CLI entry: returns process exit code.
func Main(args []string) int {
	cfg, code, err := ParseCLI(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail/sync:", err)
		return code
	}
	if cfg.Help {
		fmt.Fprintln(os.Stderr, "usage: bin/mail/sync.go [--source onlyoffice,gmail,m365,imap] [--query GMAIL_Q] [--limit N] [--offset N] [--workers N] [--force] [--raw] [--dry-run]")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	start := time.Now()
	stats, err := Run(ctx, cfg.Sync)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail/sync:", err)
		return 1
	}
	if cfg.Sync.DryRun {
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

// readEnv parses KEY=VALUE lines (ignoring comments); process env always
// wins. The file may be absent entirely — process env still applies.
func readEnv(path string) map[string]string {
	out := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
				continue
			}
			k, v, _ := strings.Cut(line, "=")
			out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), "\"'")
		}
	}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(k, "ONLYOFFICE_") || strings.HasPrefix(k, "OO_") ||
			strings.HasPrefix(k, "M365_") || strings.HasPrefix(k, "MS_") ||
			strings.HasPrefix(k, "IMAP_") {
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
