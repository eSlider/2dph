package chat

// LinkedIn session refresh: pull live cookies out of the webtop Thorium
// browser via CDP and rebuild the portable profile the LinkedIn MCP server
// (and chats/sync) consume. Go port of the former python helper.

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/websocket"
)

// RefreshLinkedInSessionOpts selects the CDP endpoint, the portable profile
// root and the container/profile to copy from.
type RefreshLinkedInSessionOpts struct {
	CDP       string // default http://127.0.0.1:9222
	Root      string // default /var/tmp/liprofile
	Container string // default work-webtop
	Profile   string // default thorium-profile
}

type cdpCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	SameSite string  `json:"sameSite"`
}

type cdpTab struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// RefreshLinkedInSession refreshes cookies.json + source-state.json under
// opts.Root from the live browser profile in the webtop container.
func RefreshLinkedInSession(opts RefreshLinkedInSessionOpts) error {
	if opts.CDP == "" {
		opts.CDP = "http://127.0.0.1:9222"
	}
	if opts.Root == "" {
		opts.Root = "/var/tmp/liprofile"
	}
	if opts.Container == "" {
		opts.Container = "work-webtop"
	}
	if opts.Profile == "" {
		opts.Profile = "thorium-profile"
	}
	profileDir := filepath.Join(opts.Root, "profile")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return err
	}

	// 1. Clear stale daemon/browser locks so the server can claim the profile.
	for _, lock := range []string{"profile-claim.lock", "profile.lock", "daemon.lock", "lease.lock"} {
		_ = os.Remove(filepath.Join(opts.Root, lock))
	}
	if ents, err := os.ReadDir(profileDir); err == nil {
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "Singleton") {
				_ = os.Remove(filepath.Join(profileDir, e.Name()))
			}
		}
	}
	if ents, err := os.ReadDir(opts.Root); err == nil {
		for _, e := range ents {
			if strings.HasPrefix(e.Name(), "invalid-state-") {
				_ = os.RemoveAll(filepath.Join(opts.Root, e.Name()))
			}
		}
	}

	// 2. Copy the live browser profile (cookies DB + Local State).
	for _, src := range []string{"Default", "Local State"} {
		dst := filepath.Join(profileDir, src)
		cmd := exec.Command("docker", "cp", opts.Container+":/config/"+opts.Profile+"/"+src, dst)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker cp %s: %w", src, err)
		}
	}
	for _, lock := range []string{"SingletonLock", "SingletonCookie", "SingletonSocket"} {
		_ = os.Remove(filepath.Join(profileDir, lock))
	}

	// 3. Pull live cookies from the running browser over CDP.
	tabs, err := cdpTabs(opts.CDP)
	if err != nil {
		return err
	}
	var wsURL string
	for _, t := range tabs {
		if t.WebSocketDebuggerURL != "" {
			wsURL = t.WebSocketDebuggerURL
			break
		}
	}
	if wsURL == "" {
		return fmt.Errorf("no CDP tab with webSocketDebuggerUrl")
	}
	cookies, err := cdpAllCookies(wsURL)
	if err != nil {
		return err
	}

	// 4. Filter linkedin domains, normalize, write cookies.json.
	out := make([]map[string]any, 0, len(cookies))
	for _, c := range cookies {
		domain := c.Domain
		if !strings.Contains(domain, "linkedin") {
			continue
		}
		switch domain {
		case ".www.linkedin.com", "www.linkedin.com":
			domain = ".linkedin.com"
		}
		expires := c.Expires
		if expires == 0 {
			expires = -1
		}
		sameSite := c.SameSite
		if sameSite == "" {
			sameSite = "None"
		}
		out = append(out, map[string]any{
			"name":     c.Name,
			"value":    strings.Trim(c.Value, `"`),
			"domain":   domain,
			"path":     orDefault(c.Path, "/"),
			"expires":  expires,
			"httpOnly": c.HTTPOnly,
			"secure":   c.Secure,
			"sameSite": sameSite,
		})
	}
	if err := writeJSONFile(filepath.Join(opts.Root, "cookies.json"), out); err != nil {
		return err
	}
	if err := writeSourceState(opts.Root, profileDir); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "refresh-session: %d linkedin cookies, profile refreshed\n", len(out))
	return nil
}

func cdpTabs(cdp string) ([]cdpTab, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(cdp, "/") + "/json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var tabs []cdpTab
	return tabs, json.NewDecoder(resp.Body).Decode(&tabs)
}

func cdpAllCookies(wsURL string) ([]cdpCookie, error) {
	ws, err := websocket.Dial(wsURL, "", "http://127.0.0.1")
	if err != nil {
		return nil, fmt.Errorf("cdp dial: %w", err)
	}
	defer ws.Close()
	req := map[string]any{"id": 1, "method": "Network.getAllCookies", "params": map[string]any{}}
	if err := websocket.JSON.Send(ws, req); err != nil {
		return nil, err
	}
	var resp struct {
		ID     int `json:"id"`
		Result struct {
			Cookies []cdpCookie `json:"cookies"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := websocket.JSON.Receive(ws, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("cdp: %s", resp.Error.Message)
	}
	return resp.Result.Cookies, nil
}

// writeSourceState writes a schema-compatible source-state.json for the
// linkedin mcp daemon (minimal fallback form).
func writeSourceState(root, profileDir string) error {
	id, err := uuid.NewRandom()
	if err != nil {
		return err
	}
	state := map[string]any{
		"version":           1,
		"source_runtime_id": "linux-amd64-host",
		"login_generation":  id.String(),
		"created_at":        time.Now().UTC().Format(time.RFC3339),
		"profile_path":      profileDir,
		"cookies_path":      filepath.Join(root, "cookies.json"),
	}
	return writeJSONFile(filepath.Join(root, "source-state.json"), state)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
