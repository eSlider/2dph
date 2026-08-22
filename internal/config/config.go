// Package config loads the typed 2dph configuration.
//
// Load stack (deep merge, highest priority last):
//
//	etc/brain/config.yml → etc/brain/config.local.yml → .env → process env
//
// Semantics: maps recurse (sub-trees combine), scalar leaves are last-write-wins,
// and every key is normalized to lower+alnum (so `Buffer-Pool`, `buffer_pool`
// and `BUFFERPOOL` all resolve to the same field). Transitional legacy env names
// (KB_*, BRAIN_SEARCH_*) map onto the same typed fields until they are retired.
//
// Note: the .env / process-env codec splits names on `_` into nested maps, so
// `SEARCH_PASS` → `search.pass` (nested) while a flat field like `WATCH_INTERVAL`
// nests as `watch.interval`. Configure flat fields via YAML (underscores strip to
// one token) or the legacy KB_* names; only underscore-free env keys (e.g.
// `WATCHINTERVAL`) reach a flat field directly.
//
// Backed by github.com/eslider/go-config codecs (env/yaml); this package only
// orchestrates the stack, optionality of the local layers, legacy mapping and
// the final decode into a typed Config.
package config

import (
	"github.com/go-viper/mapstructure/v2"
)

// Config is the merged, typed configuration for the 2dph services. Fields map
// onto lower+alnum YAML/env keys; the zero value is not meaningful — use
// Defaults() (or Load) which fills every field.
type Config struct {
	// Root is the repo root (corpus/db location). Legacy: KB_ROOT.
	Root string `mapstructure:"root"`

	// HTTP API server (pkg/httpapi / bin/brain/serve.go).
	Host    string `mapstructure:"host"`
	Port    int    `mapstructure:"port"`
	Workers int    `mapstructure:"workers"`
	Pprof   string `mapstructure:"pprof"`

	// Search backend / embedding daemon (internal/brain).
	SearchCmd        string `mapstructure:"searchcmd"` // Legacy: KB_SEARCH_CMD.
	SearchDaemonPort int    `mapstructure:"searchdaemonport"` // Legacy: KBSEARCH_PORT.
	SearchNoDaemon   bool   `mapstructure:"searchnodaemon"`   // Legacy: KBSEARCH_NO_DAEMON.
	Model            string `mapstructure:"model"`            // Legacy: KBSEARCH_MODEL.
	HFHome           string `mapstructure:"hfhome"`           // Legacy: HF_HOME.

	// Ladybug buffer pool (bytes). Legacy: KB_BUFFER_POOL.
	BufferPool int64 `mapstructure:"bufferpool"`
	// Eps is the STREAM_SANDBOX epsilon. Legacy: KBTEST_EPS.
	Eps string `mapstructure:"eps"`

	// Indexer (bin/brain/index.go). Legacy: KB_INDEX_ALLOW_LIVE.
	IndexAllowLive bool `mapstructure:"indexallowlive"`

	// Watch (bin/brain/watch.go). Legacy: KB_WATCH_INTERVAL / KB_WATCH_DIRS.
	WatchInterval int      `mapstructure:"watchinterval"`
	WatchDirs     []string `mapstructure:"watchdirs"`

	// OnlyOffice CLI override (internal/mailsync). Legacy: OO_CLI.
	OOCLI string `mapstructure:"oocli"`

	// SearXNG web search (internal/websearch). Legacy: BRAIN_SEARCH_*.
	Search SearchConfig `mapstructure:"search"`
}

// SearchConfig mirrors BRAIN_SEARCH_* (SearXNG) settings.
type SearchConfig struct {
	URL   string `mapstructure:"url"`
	User  string `mapstructure:"user"`
	Pass  string `mapstructure:"pass"`
	Cache string `mapstructure:"cache"`
	Env   string `mapstructure:"env"`
}

// Defaults returns a Config with the built-in defaults. Load() applies the
// stack on top of these, so fields absent from every layer keep a sane value.
func Defaults() Config {
	return Config{
		Host:             "127.0.0.1",
		Port:             8630,
		Workers:          4,
		SearchDaemonPort: 17830,
		BufferPool:       1 << 30,
		WatchInterval:    30,
	}
}

// decode maps a normalized config map onto dst, leaving fields absent from the
// map untouched (so defaults survive). Weak typing turns env strings into ints.
func decode(m map[string]any, dst *Config) error {
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result:           dst,
		TagName:          "mapstructure",
		WeaklyTypedInput: true,
	})
	if err != nil {
		return err
	}
	return dec.Decode(m)
}

// mergeMaps deep-merges src into dst: map values recurse, scalar leaves are
// last-write-wins. Keys are already normalized by the layer loaders.
func mergeMaps(dst, src map[string]any) {
	for k, sv := range src {
		if dv, ok := dst[k]; ok {
			if sdm, ok := sv.(map[string]any); ok {
				if ddm, ok := dv.(map[string]any); ok {
					mergeMaps(ddm, sdm)
					continue
				}
			}
		}
		dst[k] = sv
	}
}
