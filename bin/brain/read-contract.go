//usr/bin/env go run -tags=brain_readcontract "$0" "$@"; exit
//go:build brain_readcontract
//
// bin/brain/read-contract.go - read-only гейт контракта чтения brain
// (P-9.4, docs/brain/read-contract.md): проверяет формат ответов
// search/get/stats/audit против нормативных JSON-схем internal/contract.
//
//	./bin/brain/read-contract.go                         # live HTTP :8630
//	./bin/brain/read-contract.go --base http://127.0.0.1:8630 --token T
//	./bin/brain/read-contract.go --relax-version         # сервис до контракта
//	./bin/brain/read-contract.go --db                    # режим БД (см. read-contract-db.go)
//	./bin/brain/read-contract.go --json                  # машиночитаемый отчёт
//
// HTTP-режим требует сервис, собранный с contract_version в ответах; против
// старого сервиса (до контракта) используйте --relax-version — формат всё
// равно проверяется, версия уходит в warnings. Режим --db открывает kb.lbug
// read-only и проверяет данные, из которых ответы строятся (см. команду в
// read-contract-db.go).
//
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/integrii/flaggy"
)

// Client — тонкий HTTP-клиент к brain API (:8630). Формат ответов проверяется
// валидаторами internal/contract; транспорт держит только base/token/timeout.
type Client struct {
	hc    *http.Client
	base  string
	token string
}

func NewClient(base, token string) *Client {
	return &Client{
		hc:    &http.Client{Timeout: 30 * time.Second},
		base:  strings.TrimRight(base, "/"),
		token: token,
	}
}

// get выполняет GET и возвращает сырое тело (валидация — у вызывающего).
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("read-contract: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read-contract: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read-contract: GET %s: read: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read-contract: GET %s: HTTP %d: %s", path, resp.StatusCode, snippet(body))
	}
	return body, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

// --- отчёт ---

type check struct {
	Endpoint string   `json:"endpoint"`
	Pass     bool     `json:"pass"`
	Warnings []string `json:"warnings,omitempty"`
	Info     string   `json:"info,omitempty"`
}

type report struct {
	Tool    string  `json:"tool"`
	Mode    string  `json:"mode"`
	Version string  `json:"contract_version"`
	Checks  []check `json:"checks"`
	Passed  bool    `json:"passed"`
}

func (r *report) add(endpoint string, err error, warnings []string) {
	c := check{Endpoint: endpoint, Pass: err == nil, Warnings: warnings}
	if err != nil {
		c.Info = err.Error()
	} else if len(warnings) > 0 {
		c.Info = "ok (warnings)"
	} else {
		c.Info = "ok"
	}
	r.Checks = append(r.Checks, c)
}

func (r *report) passed() bool {
	for _, c := range r.Checks {
		if !c.Pass {
			return false
		}
	}
	return true
}

func (r *report) print(jsonOut bool) int {
	if jsonOut {
		r.Passed = r.passed()
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(r)
	} else {
		fmt.Printf("read contract gate: %s (contract_version=%s)\n", r.Mode, r.Version)
		for _, c := range r.Checks {
			mark := "PASS"
			if !c.Pass {
				mark = "FAIL"
			} else if len(c.Warnings) > 0 {
				mark = "PASS*"
			}
			fmt.Printf("  %-6s %-8s %s\n", mark, c.Endpoint, c.Info)
		}
		if r.passed() {
			fmt.Println("gate: PASS")
		} else {
			fmt.Println("gate: FAIL")
		}
	}
	if !r.passed() {
		return 1
	}
	return 0
}

// versionWarning — сервис без contract_version (собран до read-контракта)
// либо с несовместимой версией. При --relax-version это warning, иначе FAIL.
func versionWarning(respVersion string, relax bool) []string {
	if respVersion == "" {
		if relax {
			return []string{"service predates read-contract (no contract_version)"}
		}
		return nil // FAIL выставит валидатор
	}
	if !contract.CompatibleVersion(respVersion) {
		if relax {
			return []string{fmt.Sprintf("contract_version %q incompatible (expect %q)", respVersion, contract.ReadContractVersion)}
		}
	}
	return nil
}

// versionOnly — ошибка валидации только про contract_version: при
// --relax-version понижается до warning, формат остальных полей всё равно
// проверен.
func versionOnly(err error) bool {
	return err != nil && strings.Contains(err.Error(), "contract_version")
}

// bodyVersion достаёт contract_version из сырого тела ответа.
func bodyVersion(body []byte) string {
	var v struct {
		ContractVersion string `json:"contract_version"`
	}
	_ = json.Unmarshal(body, &v)
	return v.ContractVersion
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		base         string
		token        string
		query        string
		jsonOut      bool
		dbMode       bool
		relaxVersion bool
	)
	p := flaggy.NewParser("brain-read-contract")
	p.Description = "read-only gate: search/get/stats/audit response format (P-9.4)"
	p.String(&base, "", "base", "brain API base URL (default from config)")
	p.String(&token, "", "token", "Bearer token")
	p.String(&query, "", "query", "search query for the search check (default matrix federation)")
	p.Bool(&jsonOut, "", "json", "machine-readable report")
	p.Bool(&dbMode, "", "db", "check the live kb.lbug instead of HTTP (build with system_ladybug)")
	p.Bool(&relaxVersion, "", "relax-version", "missing/incompatible contract_version is a warning, not a failure")
	if err := cli.Parse(p, args); err != nil {
		fmt.Fprintf(os.Stderr, "brain/read-contract: %v\n", err)
		return 2
	}
	rep := report{Tool: "brain/read-contract", Version: contract.ReadContractVersion}
	if dbMode {
		return runDB(rep, jsonOut)
	}
	cfg, err := config.Load(context.Background())
	if err == nil {
		host := cfg.Host
		if host == "" {
			host = "127.0.0.1"
		}
		port := cfg.Port
		if port <= 0 {
			port = 8630
		}
		if base == "" {
			base = "http://" + host + ":" + strconv.Itoa(port)
		}
	} else if base == "" {
		base = "http://127.0.0.1:8630"
	}
	if query == "" {
		query = "matrix federation"
	}
	rep.Mode = "http:" + base
	return runHTTP(rep, base, token, query, relaxVersion, jsonOut)
}

func runHTTP(rep report, base, token, query string, relax, jsonOut bool) int {
	ctx := context.Background()
	c := NewClient(base, token)

	// Адаптеры: валидаторы возвращают (тип, error), гейту нужен только error.
	vSearch := func(b []byte) error { _, err := contract.ValidateSearchResponse(b); return err }
	vGet := func(b []byte) error { _, err := contract.ValidateGetResponse(b); return err }
	vStats := func(b []byte) error { _, err := contract.ValidateStatsResponse(b); return err }
	vAudit := func(b []byte) error { _, err := contract.ValidateAuditResponse(b); return err }

	check := func(name, path string, q url.Values, validate func([]byte) error) {
		body, err := c.get(ctx, path, q)
		if err != nil {
			rep.add(name, err, nil)
			return
		}
		if verr := validate(body); verr != nil {
			if relax && versionOnly(verr) {
				rep.add(name, nil, []string{verr.Error()})
			} else {
				rep.add(name, verr, nil)
			}
			return
		}
		rep.add(name, nil, versionWarning(bodyVersion(body), relax))
	}

	// search: общий запрос + запрос с root=facts и as_of (D24 пути).
	searchBody, err := c.get(ctx, "/search", url.Values{"q": {query}, "n": {"3"}})
	if err != nil {
		rep.add("search", err, nil)
	} else {
		var sr contract.SearchResponse
		_ = json.Unmarshal(searchBody, &sr) // lenient: нужны id даже при version-only ошибке
		if verr := vSearch(searchBody); verr != nil {
			if relax && versionOnly(verr) {
				rep.add("search", nil, []string{verr.Error()})
			} else {
				rep.add("search", verr, nil)
			}
		} else {
			rep.add("search", nil, versionWarning(bodyVersion(searchBody), relax))
		}
		// get: первый попавшийся id, с телом и без.
		checkedGet := false
		for _, h := range sr.Results {
			if h.ID == "" {
				continue
			}
			for _, body := range []bool{false, true} {
				check("get", "/get", url.Values{"id": {h.ID}, "body": {strconv.FormatBool(body)}}, vGet)
			}
			checkedGet = true
			break
		}
		if !checkedGet {
			rep.add("get", fmt.Errorf("no results to check"), nil)
		}
	}

	check("search:root=facts", "/search", url.Values{"q": {query}, "n": {"3"}, "root": {"facts"}, "as_of": {"2026-01-01"}}, vSearch)
	check("stats", "/stats", nil, vStats)
	check("audit", "/audit", nil, vAudit)
	return rep.print(jsonOut)
}

// runDB — режим БД (read-only kb.lbug). Реальная реализация в
// read-contract-db.go (cgo && system_ladybug); здесь — заглушка для cgo-free
// сборки, чтобы `go run -tags=brain_readcontract` не падал на undefined.
func runDB(rep report, jsonOut bool) int {
	fmt.Fprintln(os.Stderr, "brain/read-contract: db mode requires the cgo build:")
	fmt.Fprintln(os.Stderr, "  bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go [--json]")
	return 2
}
