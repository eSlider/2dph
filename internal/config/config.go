// Package config loads the typed 2dph configuration.
//
// Load stack (deep merge, highest priority last):
//
//	etc/brain/config.yml → etc/brain/config.local.yml → .env → process env
//
// Semantics: maps recurse (sub-trees combine), scalar leaves are last-write-wins,
// and every key is normalized to lower+alnum (so `Buffer-Pool`, `buffer_pool`
// and `BUFFERPOOL` all resolve to the same field). Transitional legacy env names
// (KB_*, BRAIN_SEARCH_*, REASONER_*) map onto the same typed fields until they
// are retired.
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
	SearchCmd        string `mapstructure:"searchcmd"`        // Legacy: KB_SEARCH_CMD.
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

	// Watch (auto re-index watcher). Legacy: KB_WATCH_INTERVAL / KB_WATCH_DIRS.
	WatchInterval int      `mapstructure:"watchinterval"`
	WatchDirs     []string `mapstructure:"watchdirs"`

	// OnlyOffice CLI override. Legacy: OO_CLI.
	OOCLI string `mapstructure:"oocli"`

	// SearXNG web search (internal/websearch). Legacy: BRAIN_SEARCH_*.
	Search SearchConfig `mapstructure:"search"`

	// Reasoner (optional LLM reasoner client). Legacy: REASONER_*.
	Reasoner ReasonerConfig `mapstructure:"reasoner"`

	// Synapse agent surface: brain leafs+edges exposed to agents.
	// Legacy: KB_SYNAPSE_*.
	Synapse SynapseConfig `mapstructure:"synapse"`

	// PST Outlook import: readpst -e →
	// .eml → the mailconv pipeline. Paths of the source .pst files are machine
	// inventory (see #79) and belong in config.local.yml, never in code.
	PST PSTConfig `mapstructure:"pst"`

	// Incubator mail ETL:
	// legacy corpora (.eml trees) → doveadm save into docker-mailserver
	// incubator owner mailboxes. Corpus roots are machine-local inventory →
	// config.local.yml.
	Incubator IncubatorConfig `mapstructure:"incubator"`

	// Vector ANN index (issue #204): approximate-nearest-neighbor search
	// outside liblbug (whose HNSW crashes, #192). Enabled serves the query
	// vector path from the index; disabled/missing index falls back to the
	// linear scan.
	Vector VectorConfig `mapstructure:"vector"`

	// Gator configures the external mail-canon importer: read-only hive root
	// of a parquet/mail canon. Machine-local path → config.local.yml,
	// никогда не хардкодится в коде.
	Gator GatorConfig `mapstructure:"gator"`
}

// GatorConfig configures read access to the external mail-canon hive.
type GatorConfig struct {
	// MailHive is the hive root of the mail canon: directory holding
	// source=mail/channel=*/dt=*/*.parquet. Empty = tools resolve it from
	// env (GATOR_MAIL_HIVE) or fail with a config error.
	MailHive string `mapstructure:"mailhive"`
}

// VectorConfig configures the vector search layer (issue #204).
type VectorConfig struct {
	ANN ANNConfig `mapstructure:"ann"`
}

// ANNConfig is the IVF index (internal/brain/ann). Empty paths default to
// <root>/var/state/vector.ann (+ ".wal"); NList/NProbe defaults come from the
// ann package (Defaults()): NList=2000, NProbe=128. Production config
// (etc/brain/config.yml) pins nprobe=2000 (full probe: recall@5=1.0, #206).
type ANNConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Index   string `mapstructure:"index"`
	Dim     int    `mapstructure:"dim"`
	NList   int    `mapstructure:"nlist"`
	NProbe  int    `mapstructure:"nprobe"`
}

// SearchConfig mirrors BRAIN_SEARCH_* (SearXNG) settings.
type SearchConfig struct {
	URL   string `mapstructure:"url"`
	User  string `mapstructure:"user"`
	Pass  string `mapstructure:"pass"`
	Cache string `mapstructure:"cache"`
	Env   string `mapstructure:"env"`
}

// ReasonerConfig mirrors REASONER_* (OpenAI-compatible reasoner client) settings.
type ReasonerConfig struct {
	BaseURL string `mapstructure:"baseurl"` // Legacy: REASONER_BASE_URL.
	Model   string `mapstructure:"model"`   // Legacy: REASONER_MODEL.
	Device  string `mapstructure:"device"`  // Legacy: REASONER_DEVICE.
}

// SynapseConfig mirrors KB_SYNAPSE_* (Synapse Matrix service) settings.
type SynapseConfig struct {
	Host  string `mapstructure:"host"`  // Legacy: KB_SYNAPSE_HOST.
	Port  int    `mapstructure:"port"`  // Legacy: KB_SYNAPSE_PORT.
	Token string `mapstructure:"token"` // Legacy: KB_SYNAPSE_TOKEN. Empty = loopback only.
}

// PSTConfig configures the Outlook PST import (#185). All paths are external
// values loaded from config (D28): the .pst inventory lives in the source map
// (#79), the readpst binary may be machine-local.
type PSTConfig struct {
	// Sources are the .pst files to import: label (corpus subdir) + path
	// (absolute, see #79). Empty = the import tool fails with a config error.
	Sources []PSTSource `mapstructure:"sources"`
	// ReadPST overrides the readpst binary path; empty = PATH lookup, then
	// the repo-local var/dist toolchain dir, then an explicit config error.
	ReadPST string `mapstructure:"readpst"`
	// Out is the corpus root for extracted mail; empty = <root>/var/corpus/mail/pst.
	Out string `mapstructure:"out"`
	// State is the source checkpoint; empty = <root>/var/state/pst.json.
	State string `mapstructure:"state"`
}

// PSTSource is one .pst archive to import: Label names the corpus subdir and
// the message source tag; Path is the absolute .pst path (see #79).
type PSTSource struct {
	Label string `mapstructure:"label"`
	Path  string `mapstructure:"path"`
}

// IncubatorConfig configures the mail incubator ETL (#252): docker transport
// plus the per-source imports (label, corpus root, doveadm owner).
type IncubatorConfig struct {
	// Docker overrides the docker binary; empty = PATH lookup.
	Docker string `mapstructure:"docker"`
	// Container is the docker-mailserver container name (default "mailserver").
	Container string `mapstructure:"container"`
	// Imports are the incubator sources; each maps one legacy corpus to one
	// owner mailbox. Empty = the tool fails with a config error.
	Imports []IncubatorImport `mapstructure:"imports"`
}

// IncubatorImport is one incubator source: Label (manifest file stem /
// report name), Source (corpus profile root, machine-local path), User (the
// doveadm mailbox owner, e.g. owner@example.com) and Owner
// (the historical address matched in headers; empty = User). When Owner is
// set, messages are routed by recipient: From=owner → Sent,
// To/CC/Delivered-To=owner → INBOX, owner nowhere → INBOX/Unmatched
// (decision 2026-09-02, #252).
type IncubatorImport struct {
	Label  string `mapstructure:"label"`
	Source string `mapstructure:"source"`
	User   string `mapstructure:"user"`
	// Owner is the historical owner address for recipient routing; empty = User.
	Owner string `mapstructure:"owner"`
	// State overrides the manifest path; empty = <root>/var/state/incubator-<label>.json.
	State string `mapstructure:"state"`
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
		Reasoner: ReasonerConfig{
			BaseURL: "http://127.0.0.1:11435/v1",
			Model:   "qwen3.5:9b",
			Device:  "cpu",
		},
		Synapse: SynapseConfig{
			Host: "127.0.0.1",
			Port: 8632,
		},
		// ANN vector search is on by default (issue #206): serve and CLI
		// search through the index, falling back to the linear scan when the
		// index is missing or corrupt. The wave's ann-build step maintains it.
		Vector: VectorConfig{
			ANN: ANNConfig{Enabled: true},
		},
		Incubator: IncubatorConfig{
			Container: "mailserver",
		},
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
