package config

import (
	"context"
	"os"
	"strings"

	"github.com/eslider/go-config/env"
)

// loadEnv reads an optional .env file plus the process environment into a
// normalized nested map. A missing .env file is skipped; the process
// environment is always the highest of these two sources.
func loadEnv(ctx context.Context, path string) (map[string]any, error) {
	var opts []env.Option
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			opts = append(opts, env.WithFile(path))
		}
	}
	opts = append(opts, env.WithCurrentEnvironment())
	return env.New(opts...).Map(ctx)
}

// legacyEnv maps transitional KB_* and BRAIN_SEARCH_* env vars onto the typed
// Config keys. It is merged last, so legacy env always wins over config files.
func legacyEnv() map[string]any {
	m := map[string]any{}
	put := func(key, name string) {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			m[key] = v
		}
	}

	put("root", "KB_ROOT")
	put("bufferpool", "KB_BUFFER_POOL")
	put("model", "KBSEARCH_MODEL")
	put("searchdaemonport", "KBSEARCH_PORT")
	put("searchnodaemon", "KBSEARCH_NO_DAEMON")
	put("eps", "KBTEST_EPS")
	put("workers", "KB_WORKERS")
	put("port", "KB_PORT")
	put("host", "KB_HOST")
	put("pprof", "KB_PPROF")
	put("searchcmd", "KB_SEARCH_CMD")
	put("indexallowlive", "KB_INDEX_ALLOW_LIVE")
	put("watchinterval", "KB_WATCH_INTERVAL")
	put("oocli", "OO_CLI")
	put("hfhome", "HF_HOME")

	if v, ok := os.LookupEnv("KB_WATCH_DIRS"); ok && v != "" {
		m["watchdirs"] = strings.Fields(v)
	}

	search := map[string]any{}
	putSearch := func(key, name string) {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			search[key] = v
		}
	}
	putSearch("url", "BRAIN_SEARCH_URL")
	putSearch("user", "BRAIN_SEARCH_USER")
	putSearch("pass", "BRAIN_SEARCH_PASS")
	putSearch("cache", "BRAIN_SEARCH_CACHE")
	putSearch("env", "BRAIN_SEARCH_ENV")
	if len(search) > 0 {
		m["search"] = search
	}

	return m
}
