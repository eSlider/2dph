//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,network_cli "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && network_cli

// usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,network_cli "$0" "$@"' "$0" "$@"; exit
//
// bin/network/network.go - network «кто с кем и через кого» из mail+commits
// графа (L-9.5 #234). Read-only. Читает Message/Person/рёбра
// SENT/TO/CC/BCC/REPLY_TO (D-1 #257) + Commit/Person/AUTHORED (L-9.4 #233)
// и выводит детерминированные связи Person↔Person: через сколько писем/
// тредов/проектов, за какой период, с premises (Message.id + gator_ref /
// Commit.id) и verdict (accept|weaken) по audit-картам Vinogradov.
//
//	./bin/network/network.go --person eslider@gmail.com          # все связи
//	./bin/network/network.go --person eslider@gmail.com --json   # машиночитаемо
//	./bin/network/network.go --person alice@x --project demo --since 2026-01-01
//	./bin/network/network.go --person alice@x --accept-only      # экспорт в CRM
//	./bin/network/network.go --person alice@x --exclude-services # без сервис-аккаунтов
//	./bin/network/network.go --person eslider@gmail.com --accept-only --person-only  # только person (N-1.5 #275)
//	KB_ROOT=/path/to/2dph ...        # db default <root>/var/kb.lbug
//
// Экспорт в CRM (ADR-0012 §сеть связей п.4): только accept-вердикты, каждая
// связь один раз (агрегат mail+git каналов) с premises на Message/Commit.
// Сервис-аккаунты (kind=service, N-1.1 #268: GitLab/PayPal/LinkedIn/
// markets-platform/трекеры/рассылки) в CRM-экспорт НЕ попадают; kind
// (person|company|service) печатается у каждой связи.
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/eSlider/2dph/internal/network"
	cliparse "github.com/eSlider/2dph/pkg/cli"
	"gopkg.in/yaml.v3"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

type netFlags struct {
	person, project, since, until, db string
	limit, depth                      int
	jsonOut, acceptOnly, all          bool
	excludeServices, personOnly       bool
}

// gitRepoRoot resolves the actual repository checkout (KB_ROOT may point the
// db at arbitrary roots for tests).
func gitRepoRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func run(args []string) int {
	var v netFlags
	p := cliparse.New("2dph network")
	p.Description = "network: «с кем и через кого» из mail+commits графа (Person↔Person, premises, verdict)"
	p.String(&v.person, "", "person", "target Person email (required)")
	p.String(&v.project, "", "project", "repo filter (git-ось): только связи с общим проектом")
	p.String(&v.since, "", "since", "RFC3339 or YYYY-MM-DD (inclusive)")
	p.String(&v.until, "", "until", "RFC3339 or YYYY-MM-DD (inclusive)")
	p.String(&v.db, "", "db", "path to kb.lbug (default <root>/var/kb.lbug)")
	p.Int(&v.limit, "", "limit", "max links (0 = all)")
	p.Int(&v.depth, "", "depth", "reserved (1 = direct links; >1 not in pilot)")
	p.Bool(&v.jsonOut, "", "json", "JSON output")
	p.Bool(&v.acceptOnly, "", "accept-only", "export only accept verdicts (CRM, ADR-0012); service links (kind=service) excluded (N-1.1 #268)")
	p.Bool(&v.excludeServices, "", "exclude-services", "drop service links (kind=service: GitLab/PayPal/LinkedIn/…, N-1.1 #268)")
	p.Bool(&v.personOnly, "", "person-only", "keep only kind=person links (N-1.5 #275: eslider@ company 494 вне скоупа CRM)")
	p.Bool(&v.all, "", "all", "include weaken links in text output (default: accept only)")
	if err := cliparse.Parse(p, args); err != nil {
		return cliparse.Fail(err)
	}
	if v.person == "" {
		fmt.Fprintln(os.Stderr, "2dph network: --person EMAIL is required")
		return 2
	}
	if v.depth > 1 {
		fmt.Fprintln(os.Stderr, "2dph network: --depth >1 не в пилоте #234 (только прямые связи)")
		return 2
	}
	sinceT, err := netParseSince(v.since)
	if err != nil {
		fmt.Fprintf(os.Stderr, "2dph network: %v\n", err)
		return 2
	}
	untilT, err := netParseSince(v.until)
	if err != nil {
		fmt.Fprintf(os.Stderr, "2dph network: %v\n", err)
		return 2
	}

	dbpath := v.db
	if dbpath == "" {
		root := os.Getenv("KB_ROOT")
		if root == "" {
			root = gitRepoRoot()
		}
		dbpath = filepath.Join(root, "var", "kb.lbug")
	}
	conn, closeConn, err := openReadOnly(dbpath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "2dph network: open %s (read-only): %v\n", dbpath, err)
		return 1
	}
	defer closeConn()

	rows, err := network.LoadRows(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "2dph network: query: %v\n", err)
		return 1
	}
	links := network.BuildLinks(rows, network.Filter{
		Person: v.person, Project: v.project, Since: sinceT, Until: untilT,
	})
	if v.limit > 0 && len(links) > v.limit {
		links = links[:v.limit]
	}
	if v.excludeServices {
		links = dropServices(links)
	}
	if v.jsonOut {
		out := links
		if v.acceptOnly {
			out = dropServices(acceptOnly(links))
		}
		if v.personOnly {
			out = network.OnlyKind(out, mailconv.KindPerson)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	if v.acceptOnly {
		links = dropServices(acceptOnly(links))
		if v.personOnly {
			links = network.OnlyKind(links, mailconv.KindPerson)
		}
		if len(links) == 0 {
			fmt.Println("2dph network: no accept links to export")
			return 0
		}
		return printCRM(v.person, links)
	}
	if len(links) == 0 {
		fmt.Println("2dph network: no links (graph empty or filters miss)")
		return 0
	}
	for i := range links {
		l := &links[i]
		if l.Verdict == "weaken" && !v.all {
			continue
		}
		printLink(l)
	}
	return 0
}

func printLink(l *network.Link) {
	who := l.Person
	if l.Name != "" {
		who = l.Name + " <" + l.Person + ">"
	}
	fmt.Printf("link person=%s kind=%s\n", who, l.Kind)
	fmt.Printf("  claim: %s\n", claim(l))
	fmt.Printf("  через: %d писем, %d тредов, %d общих получателей, %d ответов (вес %.2f)\n",
		l.Msgs, l.Threads, l.SharedCC, l.Replies, l.Weight)
	for _, pr := range l.Projects {
		fmt.Printf("  проект: %s (me %d / q %d commits, %s)\n", pr.Repo, pr.CommitsMe, pr.CommitsQ, pr.Period)
	}
	fmt.Printf("  период: %s; inference: deduction; verdict: %s\n", l.Period, l.Verdict)
	for _, g := range l.Gaps {
		fmt.Printf("  gap: %s\n", g)
	}
	maxPrem := 5
	if len(l.Premises) < maxPrem {
		maxPrem = len(l.Premises)
	}
	for _, pm := range l.Premises[:maxPrem] {
		if pm.Kind == "mail" {
			fmt.Printf("  premise: mail %s (%s)\n", pm.Ref, pm.Date)
		} else {
			fmt.Printf("  premise: commit %s (%s)\n", pm.Ref, pm.Date)
		}
	}
	if len(l.Premises) > maxPrem {
		fmt.Printf("  … +%d premises\n", len(l.Premises)-maxPrem)
	}
}

func claim(l *network.Link) string {
	if l.Name != "" {
		return fmt.Sprintf("«%s» связан(а) с target: %d писем, %d тредов, период %s", l.Name, l.Msgs, l.Threads, l.Period)
	}
	return fmt.Sprintf("%s связан(а) с target: %d писем, %d тредов, период %s", l.Person, l.Msgs, l.Threads, l.Period)
}

func acceptOnly(links []network.Link) []network.Link {
	var out []network.Link
	for _, l := range links {
		if l.Verdict == "accept" {
			out = append(out, l)
		}
	}
	return out
}

// dropServices отбрасывает сервис-аккаунты/подсистемы/рассылки
// (kind=service, N-1.1 #268) — они не деловые контакты, в CRM не идут.
func dropServices(links []network.Link) []network.Link {
	var out []network.Link
	for _, l := range links {
		if l.Kind != "service" {
			out = append(out, l)
		}
	}
	return out
}

// printCRM — YAML-экспорт accept-связей (CRM/маркетинг, ADR-0012 §сеть
// связей п.4): только проверенные связи, без дублей (агрегат mail+git).
// Premises в CRM-выводе ограничены (первые 5 + счётчик) — полный список в
// --json (аудит/машины), CRM получает ссылки-примеры для проверки.
// Тип манифеста — network.Manifest (internal/network/manifest.go): общий
// контракт с коннектором OO (bin/onlyoffice/import-network.go, N-1.2 #269).
func printCRM(target string, links []network.Link) int {
	doc := network.Manifest{Target: target, Links: []network.ManifestLink{}, Source: "2dph graph mail+git (L-9.5 #234)"}
	for _, l := range links {
		cl := network.ManifestLink{
			Person: l.Person, Name: l.Name, Kind: l.Kind, Msgs: l.Msgs, Threads: l.Threads,
			Replies: l.Replies, Period: l.Period,
		}
		for _, pr := range l.Projects {
			cl.Projects = append(cl.Projects, network.ManifestProject{Repo: pr.Repo, Period: pr.Period})
		}
		maxPrem := 5
		if len(l.Premises) < maxPrem {
			maxPrem = len(l.Premises)
		}
		cl.Premises = append(cl.Premises, l.Premises[:maxPrem]...)
		if len(l.Premises) > maxPrem {
			cl.Extra = len(l.Premises) - maxPrem
		}
		doc.Links = append(doc.Links, cl)
	}
	b, err := yaml.Marshal(doc)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Print(string(b))
	return 0
}

// openReadOnly открывает kb.lbug с ReadOnly=true — работает параллельно с
// живым RW-сервисом (Ladybug: второй RW-open невозможен, RO-open разрешён).
func openReadOnly(dbpath string) (*lbug.Connection, func(), error) {
	cfg := lbug.DefaultSystemConfig()
	cfg.ReadOnly = true
	cfg.MaxNumThreads = 8
	db, err := lbug.OpenDatabase(dbpath, cfg)
	if err != nil {
		return nil, nil, err
	}
	conn, err := lbug.OpenConnection(db)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return conn, func() {
		conn.Close()
		db.Close()
	}, nil
}

func netParseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date %q", s)
}
