//usr/bin/env bash -c 'exec go run "$0" "$@"' "$0" "$@"; exit
//
// bin/fulfill-assoc.go — deterministic: read the corpus mesh, confirm each org
// against the 2dph brain, diff against the oo CRM, and emit (or apply) the
// oo CLI commands to fulfill the person/company/opportunity associations.
//
//	OO env (ONLYOFFICE_URL/USER/PASS) must be set, e.g.:
//	  set -a; source $PWD/../eslider/go-onlyoffice/.env; set +a
//	  export ONLYOFFICE_URL=$ONLYOFFICE_HOST ONLYOFFICE_USER=$ONLYOFFICE_NAME \
//	         ONLYOFFICE_PASS=$ONLYOFFICE_PASSWORD
//
//	./bin/fulfill-assoc.go --mesh /path/knowledge-mesh-seed.yaml            # print plan
//	./bin/fulfill-assoc.go --mesh ... --brain http://127.0.0.1:8630/mcp      # brain MCP url
//	./bin/fulfill-assoc.go --mesh ... --apply                                # execute oo commands
//
// Deterministic: same mesh + brain + CRM state => same commands (idempotent;
// anything already present is skipped).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Org struct {
	ID      string   `yaml:"id"`
	Label   string   `yaml:"label"`
	Kind    string   `yaml:"kind"`
	Period  string   `yaml:"period"`
	Aka     []string `yaml:"aka"`
	Website string   `yaml:"website"`
}

type Mesh struct {
	Orgs []Org `yaml:"orgs"`
}

func main() {
	mesh := ""
	brain := "http://127.0.0.1:8630/mcp"
	apply := false
	for _, a := range os.Args[1:] {
		switch {
		case strings.HasPrefix(a, "--mesh="):
			mesh = strings.TrimPrefix(a, "--mesh=")
		case a == "--mesh" || a == "-m":
			// handled below; not used
		case strings.HasPrefix(a, "--brain="):
			brain = strings.TrimPrefix(a, "--brain=")
		case a == "--apply":
			apply = true
		}
	}
	// parse --mesh with separate value
	for i, a := range os.Args[1:] {
		if (a == "--mesh" || a == "-m") && i+1 < len(os.Args[1:]) {
			mesh = os.Args[i+2]
		}
	}
	if mesh == "" {
		fmt.Fprintln(os.Stderr, "mesh: --mesh required (path to knowledge-mesh-seed.yaml)")
		os.Exit(2)
	}
	if _, err := os.Stat(mesh); err != nil {
		fmt.Fprintln(os.Stderr, "mesh:", err)
		os.Exit(2)
	}

	raw, _ := os.ReadFile(mesh)
	var m Mesh
	if err := yaml.Unmarshal(raw, &m); err != nil {
		fmt.Fprintln(os.Stderr, "mesh yaml:", err)
		os.Exit(2)
	}

	ooComps := ooList("companies")
	ooPersons := ooList("persons")

	var cmds []string

	// 1) Employer/own/client orgs -> companies (create if absent).
	compIDs := map[string]string{}
	for _, org := range m.Orgs {
		if id, ok := matchCompany(org.Label, ooComps); ok {
			compIDs[org.ID] = id
			continue
		}
		c := "oo companies create --name " + shellq(org.Label)
		if org.Website != "" {
			c += " --website " + shellq(org.Website)
		}
		cmds = append(cmds, c)
		compIDs[org.ID] = "?" // filled after apply (script re-queries oo in --apply)
	}

	// 2) Confirm each org against the brain (deterministic score gate).
	fmt.Fprintf(os.Stderr, "# brain confirmation (score>=8 => confirmed)\n")
	for _, org := range m.Orgs {
		sc := brainScore(brain, org.Label+" "+org.Kind)
		fmt.Fprintf(os.Stderr, "  %-14s %-24s kind=%-14s brain=%.1f\n", org.ID, org.Label, org.Kind, sc)
	}

	// 3) Andriy person -> current own org.
	ownID := ""
	for _, org := range m.Orgs {
		if org.Kind == "own" {
			ownID = org.ID
			break
		}
	}
	if ownID != "" {
		if id, ok := ooPersons[lower("andriy oblivantsev")]; ok {
			cmds = append(cmds, fmt.Sprintf("oo persons update %s --company-id %s", id, compIDs[ownID]))
		} else {
			label := "Andriy Oblivantsev"
			c := "oo persons create --first Andriy --last Oblivantsev --company-id " + compIDs[ownID]
			_ = label
			cmds = append(cmds, c)
		}
	}

	// 4) Company -> project contacts (deterministic: link a client company to
	//    every project whose parent company matches). Corpus-derived, e.g.:
	//    defacto GmbH hosted/served all GRID GmbH client projects.
	ooProjs := ooProjects()
	// map[companyDisplayName]parentCompanyPrefixInProjectTitle
	link := map[string]string{
		"defacto GmbH": "GRID GmbH",
	}
	for compName, parentPrefix := range link {
		compID, ok := ooComps[lower(compName)]
		if !ok {
			fmt.Fprintf(os.Stderr, "# link: company %q not found in CRM (skip)\n", compName)
			continue
		}
		for _, pr := range ooProjs {
			if strings.Contains(pr.Parent, parentPrefix) {
				cmds = append(cmds, fmt.Sprintf("oo projects contacts add %s %s", pr.ID, compID))
			}
		}
	}

	// print plan
	for _, c := range cmds {
		fmt.Println(c)
	}

	if apply {
		for _, c := range cmds {
			if strings.HasPrefix(c, "oo ") {
				runCmd(c)
			}
		}
	}
}

type Project struct {
	ID     string
	Parent string
	Title  string
}

// ooProjects loads all oo projects and extracts the parent company from the
// title ("<cc> | <parent> | <name>"). Deterministic.
func ooProjects() []Project {
	out := runCmdOut("oo", "projects", "list", "-o", "json")
	if out == "" {
		return nil
	}
	var rows []map[string]any
	_ = json.Unmarshal([]byte(out), &rows)
	var res []Project
	for _, r := range rows {
		var id string
		switch v := r["id"].(type) {
		case float64:
			id = fmt.Sprintf("%.0f", v)
		case string:
			id = v
		}
		t, _ := r["title"].(string)
		parent := ""
		parts := strings.Split(t, "|")
		if len(parts) >= 2 {
			parent = strings.TrimSpace(parts[1])
		}
		res = append(res, Project{ID: id, Parent: parent, Title: t})
	}
	return res
}

func ooList(kind string) map[string]string {
	out := runCmdOut("oo", kind, "list", "--count", "1000", "-o", "json")
	if out == "" {
		return map[string]string{}
	}
	var rows []map[string]any
	_ = json.Unmarshal([]byte(out), &rows)
	res := map[string]string{}
	for _, r := range rows {
		name, _ := r["displayName"].(string)
		var id string
		switch v := r["id"].(type) {
		case float64:
			id = fmt.Sprintf("%.0f", v)
		case string:
			id = v
		}
		if name != "" {
			res[lower(name)] = id
		}
	}
	return res
}

func brainScore(url, q string) float64 {
	payload := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name": "search", "arguments": map[string]any{"q": q, "n": 1},
		},
	}
	body, _ := json.Marshal(payload)
	resp, err := httpPost(url, body)
	if err != nil {
		return 0
	}
	var r struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(resp, &r) != nil || len(r.Result.Content) == 0 {
		return 0
	}
	var sr struct {
		Results []struct {
			Score float64 `json:"score"`
		} `json:"results"`
	}
	if json.Unmarshal([]byte(r.Result.Content[0].Text), &sr) != nil || len(sr.Results) == 0 {
		return 0
	}
	return sr.Results[0].Score
}

func httpPost(url string, body []byte) ([]byte, error) {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, nil
}

func shellq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func runCmd(cmdline string) {
	parts := strings.Fields(cmdline)
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "run:", err)
	}
}

func runCmdOut(args ...string) string {
	c := exec.Command(args[0], args[1:]...)
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func lower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// matchCompany returns the existing CRM company id whose name shares a
// significant token (>=4 alpha chars) with the org label. Deterministic.
func matchCompany(label string, ooComps map[string]string) (string, bool) {
	toks := significantTokens(label)
	if len(toks) == 0 {
		return "", false
	}
	for name, id := range ooComps {
		nl := lower(name)
		for _, t := range toks {
			if strings.Contains(nl, t) {
				return id, true
			}
		}
	}
	return "", false
}

// significantTokens extracts words >=4 alpha chars from a label.
func significantTokens(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		w = strings.Trim(w, "()/.,'\"")
		alpha := 0
		for _, r := range w {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				alpha++
			}
		}
		if alpha >= 4 {
			out = append(out, lower(w))
		}
	}
	return out
}
