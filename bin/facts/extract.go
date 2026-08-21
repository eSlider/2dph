//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,facts_extract "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && facts_extract
//
// bin/facts/extract.go - acquire confirmed facts from the ops stack (2-source each).
//
//	./bin/facts/extract.go [--json] [--dry-run] [--ssh PATH] [--compose PATH]
//
// The deduction rule: a fact is only stored under root=facts if it is backed by
// >=2 independent sources. Sources here:
//
//	S1 runtime  : docker ps (running containers)  or ~/.ssh/config (hosts)
//	S2 declared : docker-compose files             or PLAN.md/README.md mentions
//
// Extracted facts are written into var/kb.lbug (root=facts, confidence=confirmed).
// --dry-run prints the proposed facts without touching the database.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/pkg/cli"
)

const repoID = "eSlider/2dph"

var (
	docMarkers   = []string{"README.md", "PLAN.md", "AGENTS.md"}
	composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
	sshConfig    = defaultSSHConfig()
)

func defaultSSHConfig() string {
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".ssh", "config")
	}
	return ".ssh/config"
}

type fact struct {
	Text   string `json:"text"`
	Source string `json:"source"`
	Loc    string `json:"loc"`
	How    string `json:"how"`
}

func dockerPS() []string {
	out, err := exec.Command("docker", "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(l); s != "" {
			names = append(names, s)
		}
	}
	return names
}

func containerComposeDir(name string) string {
	out, err := exec.Command("docker", "inspect", name,
		"--format", `{{ index .Config.Labels "com.docker.compose.project.working_dir"}}`).Output()
	if err != nil {
		return ""
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" || dir == "<no value>" {
		return ""
	}
	return dir
}

func composeServices(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Services map[string]any `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	var out []string
	for k := range doc.Services {
		out = append(out, k)
	}
	return out
}

func composeFilesIn(dir string) []string {
	if dir == "" {
		return nil
	}
	var out []string
	for _, n := range composeNames {
		p := filepath.Join(dir, n)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

func sshHosts(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	re := regexp.MustCompile(`^\s*Host\s+(.+)$`)
	var hosts []string
	for _, line := range strings.Split(string(data), "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			for _, h := range strings.Fields(m[1]) {
				if h != "*" {
					hosts = append(hosts, h)
				}
			}
		}
	}
	return hosts
}

func mentions(term string, files []string) bool {
	re, err := regexp.Compile(`(?i)\b` + regexp.QuoteMeta(term) + `\b`)
	if err != nil {
		return false
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if re.Match(data) {
			return true
		}
	}
	return false
}

func buildFacts(composeFiles []string) []fact {
	root := brain.RepoRoot()
	docFiles := make([]string, len(docMarkers))
	for i, m := range docMarkers {
		docFiles[i] = filepath.Join(root, m)
	}

	facts := []fact{}
	running := dockerPS()
	// Pair each running container against its own compose file (2 independent
	// sources: runtime state docker ps x declared state compose).
	runtimeFacts := 0
	for _, name := range running {
		cdir := containerComposeDir(name)
		for _, cfile := range composeFilesIn(cdir) {
			declared := false
			for _, s := range composeServices(cfile) {
				if s == name {
					declared = true
					break
				}
			}
			if declared {
				facts = append(facts, fact{
					Text:   fmt.Sprintf("container '%s' is running and declared in %s", name, filepath.Base(cfile)),
					Source: fmt.Sprintf("docker ps x compose:%s", filepath.Base(cfile)),
					Loc:    fmt.Sprintf("%s:%s", cfile, name),
					How:    "facts/extract",
				})
				runtimeFacts++
				break
			}
		}
	}
	if runtimeFacts > 0 {
		fmt.Fprintf(os.Stderr, "facts/extract: paired %d/%d running containers to compose\n", runtimeFacts, len(running))
	}
	composeSvc := map[string]bool{}
	for _, c := range composeFiles {
		for _, s := range composeServices(c) {
			composeSvc[s] = true
		}
	}
	if len(composeSvc) > 0 && len(running) > 0 {
		var overlap []string
		for _, name := range running {
			if composeSvc[name] {
				overlap = append(overlap, name)
			}
		}
		sort.Strings(overlap)
		first := ""
		if len(composeFiles) > 0 {
			first = filepath.Base(composeFiles[0])
		}
		for _, name := range overlap {
			facts = append(facts, fact{
				Text:   fmt.Sprintf("container '%s' is running and declared in compose", name),
				Source: fmt.Sprintf("docker ps x %s", first),
				Loc:    "docker ps; docker compose config",
				How:    "facts/extract",
			})
		}
	}

	hosts := sshHosts(sshConfig)
	for _, host := range hosts {
		if mentions(host, docFiles) {
			facts = append(facts, fact{
				Text:   fmt.Sprintf("host '%s' is configured in ~/.ssh/config and referenced in this repo", host),
				Source: fmt.Sprintf("ssh config x docs(%s)", strings.Join(docMarkers, ", ")),
				Loc:    fmt.Sprintf("~/.ssh/config:%s", host),
				How:    "facts/extract",
			})
		}
	}

	// Single-docker-container facts still need 2 sources: running + hostname hint.
	if len(hosts) > 0 {
		known := map[string]bool{}
		for _, h := range hosts {
			known[h] = true
		}
		for _, name := range running {
			if known[name] {
				facts = append(facts, fact{
					Text:   fmt.Sprintf("container '%s' is running and matches configured host '%s'", name, name),
					Source: "docker ps x ssh config",
					Loc:    fmt.Sprintf("docker ps:%s; ~/.ssh/config:%s", name, name),
					How:    "facts/extract",
				})
			}
		}
	}
	return facts
}

func dedupe(facts []fact) []fact {
	seen := map[string]bool{}
	out := []fact{}
	for _, f := range facts {
		if seen[f.Text] {
			continue
		}
		seen[f.Text] = true
		out = append(out, f)
	}
	return out
}

func writeFacts(facts []fact) error {
	model, err := brain.LoadModel()
	if err != nil {
		return fmt.Errorf("model: %w", err)
	}
	defer model.Close()
	db, conn, err := brain.OpenWritable("")
	if err != nil {
		return err
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	for _, f := range facts {
		emb, err := model.Embed(f.Text)
		if err != nil {
			return fmt.Errorf("embed: %w", err)
		}
		if _, err := brain.UpsertLeaf(conn, brain.LeafInput{
			Text: f.Text, Root: "facts", Confidence: "confirmed",
			Source: f.Source, SourceRev: repoID, How: f.How, Loc: f.Loc, Type: "fact",
			Embedding: emb,
		}); err != nil {
			return err
		}
	}
	// Upsert-with-index is safe; never DROP+recreate (ghost catalog kills HNSW).
	if err := brain.EnsureIndexes(conn); err != nil {
		return fmt.Errorf("indexes: %w", err)
	}
	return nil
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		jsonOut  bool
		dryRun   bool
		sshPath  string
		composes []string
	)
	args = filterComposeArgs(args, &composes)
	p := cli.New("facts-extract")
	p.Description = "acquire confirmed facts from ops sources (2-source each)"
	p.Bool(&jsonOut, "", "json", "JSON output")
	p.Bool(&dryRun, "", "dry-run", "print proposed facts, write nothing")
	p.String(&sshPath, "", "ssh", "path to ssh config")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}

	if sshPath != "" {
		sshConfig = sshPath
	}
	composeFiles := composes
	if len(composeFiles) == 0 {
		root := brain.RepoRoot()
		composeFiles = []string{
			filepath.Join(root, "docker", "compose.yaml"),
			filepath.Join(root, "compose.yaml"),
		}
	}

	facts := dedupe(buildFacts(composeFiles))
	if !dryRun && len(facts) > 0 {
		if err := writeFacts(facts); err != nil {
			fmt.Fprintf(os.Stderr, "facts/extract: %v\n", err)
			return 1
		}
	}

	out := map[string]any{"count": len(facts), "facts": facts}
	if jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(out)
	} else {
		y, err := yaml.Marshal(out)
		if err != nil {
			return 1
		}
		fmt.Print(string(y))
	}
	return 0
}

// filterComposeArgs strips repeatable --compose flags into out (flaggy wrapper
// has no slice flag).
func filterComposeArgs(args []string, out *[]string) []string {
	res := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--compose" && i+1 < len(args):
			*out = append(*out, args[i+1])
			i++
		case strings.HasPrefix(a, "--compose="):
			*out = append(*out, strings.TrimPrefix(a, "--compose="))
		default:
			res = append(res, a)
		}
	}
	return res
}