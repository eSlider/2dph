package websearch

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	URL  string
	User string
	Pass string
}

func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("no credentials at %s (mode 600, BRAIN_SEARCH_URL)", path)
	}
	conf := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"'`)
		conf[strings.TrimSpace(k)] = v
	}
	out := Config{
		URL:  conf["BRAIN_SEARCH_URL"],
		User: conf["BRAIN_SEARCH_USER"],
		Pass: conf["BRAIN_SEARCH_PASS"],
	}
	if out.URL == "" {
		return Config{}, fmt.Errorf("%s is missing BRAIN_SEARCH_URL", path)
	}
	return out, nil
}

func Fetch(client *http.Client, conf Config, query string, params map[string]string, timeout time.Duration) (Payload, error) {
	if client == nil {
		client = &http.Client{Timeout: timeout}
	} else if timeout > 0 {
		c := *client
		c.Timeout = timeout
		client = &c
	}
	q := url.Values{}
	q.Set("q", query)
	q.Set("format", "json")
	for k, v := range params {
		if v != "" {
			q.Set(k, v)
		}
	}
	u := strings.TrimRight(conf.URL, "/") + "/search?" + q.Encode()
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return Payload{}, err
	}
	if conf.User != "" || conf.Pass != "" {
		token := base64.StdEncoding.EncodeToString([]byte(conf.User + ":" + conf.Pass))
		req.Header.Set("Authorization", "Basic "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Payload{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Payload{}, err
	}
	if resp.StatusCode >= 400 {
		return Payload{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var p Payload
	if err := json.Unmarshal(body, &p); err != nil {
		return Payload{}, err
	}
	return p, nil
}
