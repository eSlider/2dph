//usr/bin/env go run "$0" "$@"; exit
// bin/onlyoffice/import-contact.go - read address-book files and reconcile
// each contact into the OnlyOffice CRM (best effort; skip existing by email).
//
//	OO_URL=… OO_USER=… OO_PASSWORD=… ./bin/onlyoffice/import-contact.go --sources a.csv
//	./bin/onlyoffice/import-contact.go --sources dir/ --dry-run
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/eSlider/2dph/pkg/cli"
	"github.com/eSlider/2dph/pkg/contact"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	var (
		sources []string
		dryRun  bool
	)
	p := cli.New("onlyoffice-import-contact")
	p.Description = "reconcile address-book contacts into the OnlyOffice CRM"
	p.StringSlice(&sources, "s", "sources", "comma-separated files or dirs to read")
	p.Bool(&dryRun, "", "dry-run", "print parsed counts only, write nothing")
	if err := cli.Parse(p, args); err != nil {
		return cli.Fail(err)
	}
	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "onlyoffice-import-contact: --sources is required")
		return 2
	}
	var srcs []string
	for _, s := range sources {
		for _, part := range strings.Split(s, ",") {
			if p := strings.TrimSpace(part); p != "" {
				srcs = append(srcs, p)
			}
		}
	}
	cs, err := contact.Load(srcs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "onlyoffice-import-contact: %v\n", err)
		return 1
	}
	cs = contact.Dedupe(cs)
	contact.PrintCounts(cs)
	if dryRun {
		return 0
	}
	created, matched, failed, err := writeOO(context.Background(), cs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "onlyoffice-import-contact: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "oo: created=%d matched=%d failed=%d\n", created, matched, failed)
	return 0
}

type ooConfig struct{ URL, User, Password string }

type ooClient struct {
	cfg    ooConfig
	client *http.Client
	token  string
}

func ooConfigFromEnv() (ooConfig, error) {
	pick := func(a, b string) string {
		if v := strings.TrimSpace(os.Getenv(a)); v != "" {
			return v
		}
		return strings.TrimSpace(os.Getenv(b))
	}
	cfg := ooConfig{
		URL:      pick("ONLYOFFICE_URL", "OO_URL"),
		User:     pick("ONLYOFFICE_USER", "OO_USER"),
		Password: pick("ONLYOFFICE_PASS", "OO_PASSWORD"),
	}
	if cfg.URL == "" || cfg.User == "" || cfg.Password == "" {
		return cfg, fmt.Errorf("needs ONLYOFFICE_URL/USER/PASS (or OO_URL/USER/PASSWORD) env vars")
	}
	return cfg, nil
}

func newOOClient(cfg ooConfig) (*ooClient, error) {
	jar, _ := cookiejar.New(nil)
	c := &ooClient{cfg: cfg, client: &http.Client{Jar: jar, Timeout: 60 * time.Second}}
	body, _ := json.Marshal(map[string]any{"userName": cfg.User, "password": cfg.Password, "type": 0})
	resp, err := c.client.Post(strings.TrimRight(cfg.URL, "/")+"/api/2.0/authentication.json",
		"application/json", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("oo authenticate status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var out struct {
		Response struct {
			Token string `json:"token"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Response.Token == "" {
		return nil, fmt.Errorf("oo authenticate: empty token")
	}
	c.token = out.Response.Token
	return c, nil
}

func writeOO(ctx context.Context, cs []contact.Contact) (created, matched, failed int, err error) {
	cfg, err := ooConfigFromEnv()
	if err != nil {
		return 0, 0, 0, err
	}
	c, err := newOOClient(cfg)
	if err != nil {
		return 0, 0, 0, err
	}
	base := strings.TrimRight(cfg.URL, "/")
	for _, ct := range cs {
		email := first(ct.Emails)
		if _, found, ferr := c.findPersonByEmail(ctx, base, email); ferr != nil {
			failed++
			fmt.Fprintf(os.Stderr, "oo lookup %q: %v\n", ct.DisplayName(), ferr)
			continue
		} else if found {
			matched++
			fmt.Fprintf(os.Stderr, "oo matched %q (%s)\n", ct.DisplayName(), email)
			continue
		}
		pid, cerr := c.createPerson(ctx, base, ct)
		if cerr != nil {
			failed++
			fmt.Fprintf(os.Stderr, "oo create %q: %v\n", ct.DisplayName(), cerr)
			continue
		}
		created++
		fmt.Fprintf(os.Stderr, "oo created %q -> %s\n", ct.DisplayName(), pid)
	}
	return created, matched, failed, nil
}

func first(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

func (c *ooClient) findPersonByEmail(ctx context.Context, base, email string) (string, bool, error) {
	if email == "" {
		return "", false, nil
	}
	u := base + "/api/2.0/crm/contact/filter.json?search=" + url.QueryEscape(email)
	var out struct {
		Response []map[string]any `json:"response"`
	}
	if err := c.get(ctx, u, &out); err != nil {
		return "", false, err
	}
	for _, row := range out.Response {
		if isCompany(row) {
			continue
		}
		for _, k := range []string{"email", "primaryEmail"} {
			if strings.EqualFold(strings.TrimSpace(fmt.Sprint(row[k])), email) {
				return fmt.Sprint(row["id"]), true, nil
			}
		}
	}
	return "", false, nil
}

func (c *ooClient) createPerson(ctx context.Context, base string, ct contact.Contact) (string, error) {
	form := url.Values{}
	form.Set("firstName", ct.Given)
	form.Set("lastName", ct.Family)
	if form.Get("firstName") == "" && form.Get("lastName") == "" {
		form.Set("firstName", ct.DisplayName())
	}
	if ct.Title != "" {
		form.Set("jobTitle", ct.Title)
	}
	if ct.Org != "" {
		form.Set("about", ct.Org)
	}
	var out struct {
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if err := c.postForm(ctx, base+"/api/2.0/crm/contact/person.json", form, &out); err != nil {
		return "", err
	}
	id := out.Response.ID
	for _, e := range ct.Emails {
		_ = c.addContactInfo(ctx, base, id, "email", e, "Work", e == first(ct.Emails))
	}
	for _, p := range ct.Phones {
		_ = c.addContactInfo(ctx, base, id, "phone", p, "Work", false)
	}
	return id, nil
}

func (c *ooClient) addContactInfo(ctx context.Context, base, id, infoType, data, category string, primary bool) error {
	form := url.Values{}
	form.Set("infoType", infoType)
	form.Set("data", data)
	form.Set("category", category)
	form.Set("isPrimary", fmt.Sprint(primary))
	var out any
	return c.postForm(ctx, fmt.Sprintf("%s/api/2.0/crm/contact/%s/data.json", base, url.PathEscape(id)), form, &out)
}

func (c *ooClient) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	return c.do(req, out)
}

func (c *ooClient) postForm(ctx context.Context, u string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	return c.do(req, out)
}

func (c *ooClient) do(req *http.Request, out any) error {
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("oo %s %s: status %d: %s", req.Method, req.URL.Path, resp.StatusCode, truncate(string(data), 300))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func isCompany(m map[string]any) bool {
	v, ok := m["isCompany"].(bool)
	return ok && v
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
