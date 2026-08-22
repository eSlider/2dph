package websearch

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const (
	StatusSkipped = "skipped"
	StatusRefused = "refused"
)

type LookupOpt struct {
	Limit     int
	Timeout   time.Duration
	EnvPath   string
	CachePath string
	Client    *http.Client
	Now       func() float64
	Sleep     func(context.Context, time.Duration) error
}

// defaultCache and defaultEnv carry the typed-config SearXNG paths (set via
// SetDefaults). Lookup falls back to them when LookupOpt leaves them empty.
var (
	defaultCache string
	defaultEnv   string
)

// SetDefaults wires the cache and credentials-env paths from the typed config
// (internal/config Search.Cache / Search.Env) so Lookup keeps working without
// reading the environment directly.
func SetDefaults(cachePath, envPath string) {
	defaultCache = cachePath
	defaultEnv = envPath
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

func Lookup(ctx context.Context, query string, opt LookupOpt) Output {
	if ctx == nil {
		ctx = context.Background()
	}
	if opt.Limit <= 0 {
		opt.Limit = DefaultLimit
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 25 * time.Second
	}
	nowFn := opt.Now
	if nowFn == nil {
		nowFn = func() float64 { return float64(time.Now().Unix()) }
	}
	sleepFn := opt.Sleep
	if sleepFn == nil {
		sleepFn = func(ctx context.Context, d time.Duration) error {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-t.C:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	if reason := PHIReason(query); reason != "" {
		return Output{Query: query, Status: StatusRefused, Note: reason}
	}

	cachePath := opt.CachePath
	if cachePath == "" {
		cachePath = defaultCache
	}
	if cachePath == "" {
		cachePath = homeDir() + "/.cache/brain/web-search.sqlite"
	}
	cache, err := OpenCache(cachePath)
	if err != nil {
		return Output{Query: query, Status: StatusSkipped, Note: "cache: " + err.Error()}
	}
	defer cache.Close()

	key := CacheKey(query, nil)
	now := nowFn()
	if cached, err := cache.Get(key, CacheTTL, now); err == nil && cached != nil {
		out := Project(*cached, opt.Limit, DefaultSnippetChars)
		out.Cached = true
		return out
	}

	envPath := opt.EnvPath
	if envPath == "" {
		envPath = defaultEnv
	}
	if envPath == "" {
		envPath = homeDir() + "/.config/brain/search.env"
	}
	conf, err := LoadConfig(envPath)
	if err != nil {
		return Output{Query: query, Status: StatusSkipped, Note: "no BRAIN_SEARCH_URL; second source not consulted"}
	}

	lock, err := os.OpenFile(cachePath+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Output{Query: query, Status: StatusSkipped, Note: "lock: " + err.Error()}
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		return Output{Query: query, Status: StatusSkipped, Note: "lock: " + err.Error()}
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)

	last, err := cache.LastCall()
	if err != nil {
		return Output{Query: query, Status: StatusSkipped, Note: "cache: " + err.Error()}
	}
	if delay := WaitFor(last, nowFn(), MinInterval); delay > 0 {
		if err := sleepFn(ctx, time.Duration(delay*float64(time.Second))); err != nil {
			return Output{Query: query, Status: StatusSkipped, Note: "cancelled"}
		}
	}
	_ = cache.MarkCall(nowFn())

	payload, err := Fetch(opt.Client, conf, query, nil, opt.Timeout)
	if err != nil {
		return Output{Query: query, Status: StatusThrottled, Note: fmt.Sprintf("request failed: %v", err)}
	}
	if Classify(payload) == StatusOK {
		_ = cache.Put(key, payload, nowFn())
	}
	return Project(payload, opt.Limit, DefaultSnippetChars)
}
