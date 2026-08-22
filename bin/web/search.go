//usr/bin/env go run "$0" "$@"; exit
//
// bin/web/search.go - SearXNG as the second independent source (D3).
//
//	./bin/web/search.go "LadybugDB vector search"
//	./bin/web/search.go "model2vec" --category it --json
//	./bin/web/search.go "postgres" --site github.com --fresh year
//
// Empty results mean throttled, not "nothing exists". Exit 2 = PII refuse, 3 = throttled.
// Config: $BRAIN_SEARCH_ENV (default $HOME/.config/brain/search.env).
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/websearch"
	"github.com/eSlider/2dph/pkg/cli"
	"golang.org/x/sys/unix"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	cfg, err := config.Load(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "web/search: config: %v\n", err)
		return 1
	}
	c, err := websearch.ParseArgs(args)
	if err != nil {
		return cli.Fail(err)
	}
	query, site, lang, fresh, category, engines := c.Query, c.Site, c.Lang, c.Fresh, c.Category, c.Engines
	limit := c.Limit
	jsonOut, refresh, force := c.JSONOut, c.Refresh, c.Force
	ttl := c.TTL
	timeout := c.Timeout
	if site != "" {
		query = "site:" + site + " " + query
	}
	if reason := websearch.PHIReason(query); reason != "" && !force {
		fmt.Fprintf(os.Stderr, "refused: %s. This query would leave the host.\n", reason)
		fmt.Fprintln(os.Stderr, "Rephrase without identifiers, or pass --force if it is genuinely public.")
		return 2
	}

	params := map[string]string{}
	if lang != "" {
		params["language"] = lang
	}
	if fresh != "" {
		params["time_range"] = fresh
	}
	if category != "" {
		params["categories"] = category
	}
	if engines != "" {
		params["engines"] = engines
	}

	cachePath := cfg.Search.Cache
	if cachePath == "" {
		if hd, err := os.UserHomeDir(); err == nil {
			cachePath = hd + "/.cache/brain/web-search.sqlite"
		}
	}
	cache, err := websearch.OpenCache(cachePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "web/search: cache: %v\n", err)
		return 1
	}
	defer cache.Close()

	key := websearch.CacheKey(query, params)
	now := float64(time.Now().Unix())
	if !refresh {
		if cached, err := cache.Get(key, ttl, now); err != nil {
			fmt.Fprintf(os.Stderr, "web/search: cache: %v\n", err)
			return 1
		} else if cached != nil {
			out := websearch.Project(*cached, limit, websearch.DefaultSnippetChars)
			out.Cached = true
			return writeOut(out, jsonOut)
		}
	}

	envPath := cfg.Search.Env
	if envPath == "" {
		if hd, err := os.UserHomeDir(); err == nil {
			envPath = hd + "/.config/brain/search.env"
		}
	}
	conf, err := websearch.LoadConfig(envPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "web/search: %v\n", err)
		return 1
	}

	lockPath := cachePath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "web/search: lock: %v\n", err)
		return 1
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		fmt.Fprintf(os.Stderr, "web/search: lock: %v\n", err)
		return 1
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)

	var payload websearch.Payload
	attempts := 1 + len(websearch.RetryBackoff)
	client := &http.Client{}
	for attempt := 0; attempt < attempts; attempt++ {
		last, err := cache.LastCall()
		if err != nil {
			fmt.Fprintf(os.Stderr, "web/search: cache: %v\n", err)
			return 1
		}
		if delay := websearch.WaitFor(last, float64(time.Now().Unix()), websearch.MinInterval); delay > 0 {
			time.Sleep(time.Duration(delay * float64(time.Second)))
		}
		if err := cache.MarkCall(float64(time.Now().Unix())); err != nil {
			fmt.Fprintf(os.Stderr, "web/search: cache: %v\n", err)
			return 1
		}
		payload, err = websearch.Fetch(client, conf, query, params, time.Duration(timeout)*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "request failed: %v\n", err)
			return 3
		}
		if websearch.Classify(payload) == websearch.StatusOK {
			break
		}
		if attempt < len(websearch.RetryBackoff) {
			time.Sleep(time.Duration(websearch.RetryBackoff[attempt] * float64(time.Second)))
		}
	}

	if websearch.Classify(payload) == websearch.StatusOK {
		if err := cache.Put(key, payload, float64(time.Now().Unix())); err != nil {
			fmt.Fprintf(os.Stderr, "web/search: cache: %v\n", err)
		}
	}
	out := websearch.Project(payload, limit, websearch.DefaultSnippetChars)
	code := writeOut(out, jsonOut)
	if out.Status != websearch.StatusOK && code == 0 {
		return 3
	}
	return code
}

func writeOut(out websearch.Output, jsonOut bool) int {
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(out); err != nil {
			return 1
		}
		if out.Status != websearch.StatusOK {
			return 3
		}
		return 0
	}
	fmt.Print(out.YAML())
	if out.Status != websearch.StatusOK {
		return 3
	}
	return 0
}
