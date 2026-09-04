package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// isolateEnv unsets process-env variables that the load stack's env codec
// (WithCurrentEnvironment, merged highest-priority) would otherwise let
// override the testdata fixtures. Without this, ambient CI runner env vars
// (e.g. SEARCH_PASS=) make the .env-layer assertions non-hermetic.
func isolateEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		// fixture keys
		"SEARCH_PASS", "WATCHINTERVAL",
		// legacy names mapped by legacyEnv()
		"KB_ROOT", "KB_PORT", "KB_HOST", "KB_WORKERS", "KB_PPROF",
		"KB_SEARCH_CMD", "KBSEARCH_PORT", "KBSEARCH_NO_DAEMON",
		"KBSEARCH_MODEL", "HF_HOME", "KB_BUFFER_POOL", "KBTEST_EPS",
		"KB_INDEX_ALLOW_LIVE", "KB_WATCH_INTERVAL", "KB_WATCH_DIRS",
		"OO_CLI", "BRAIN_SEARCH_URL", "BRAIN_SEARCH_USER",
		"BRAIN_SEARCH_PASS", "BRAIN_SEARCH_CACHE", "BRAIN_SEARCH_ENV",
		"REASONER_BASE_URL", "REASONER_MODEL", "REASONER_DEVICE",
		"VECTOR_ANN_ENABLED", "VECTOR_ANN_INDEX", "VECTOR_ANN_DIM",
		"VECTOR_ANN_NLIST", "VECTOR_ANN_NPROBE",
	}
	prev := map[string]string{}
	for _, k := range vars {
		prev[k], _ = os.LookupEnv(k)
		os.Unsetenv(k)
	}
	t.Cleanup(func() {
		for k, v := range prev {
			if v != "" {
				os.Setenv(k, v)
			} else {
				os.Unsetenv(k)
			}
		}
	})
}

func loadTest(t *testing.T, dir string) *Config {
	t.Helper()
	cfg, err := LoadFrom(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestLoadStack_DeepMergeAndPriority checks the load stack
// (config.yml → config.local.yml → .env) with deep merge: maps recurse
// (search.* combines across layers) and scalar leaves are last-write-wins.
func TestLoadStack_DeepMergeAndPriority(t *testing.T) {
	isolateEnv(t)
	cfg := loadTest(t, filepath.Join("testdata", "basic"))

	if cfg.Root != "/srv/2dph" {
		t.Fatalf("root = %q, want /srv/2dph (config.yml)", cfg.Root)
	}
	if cfg.Port != 9100 {
		t.Fatalf("port = %d, want 9100 (config.local.yml overrides config.yml)", cfg.Port)
	}
	if cfg.Workers != 4 {
		t.Fatalf("workers = %d, want 4 (config.yml)", cfg.Workers)
	}
	// Maps recurse: url from base, user from local, pass from .env merge into search.*.
	if cfg.Search.URL != "https://search.example.com" {
		t.Fatalf("search.url = %q, want base value", cfg.Search.URL)
	}
	if cfg.Search.User != "alice" {
		t.Fatalf("search.user = %q, want alice (config.local.yml)", cfg.Search.User)
	}
	if cfg.Search.Pass != "secretpw" {
		t.Fatalf("search.pass = %q, want secretpw (.env)", cfg.Search.Pass)
	}
	if cfg.WatchInterval != 15 {
		t.Fatalf("watch_interval = %d, want 15 (.env)", cfg.WatchInterval)
	}
	if cfg.BufferPool != 1<<30 {
		t.Fatalf("buffer_pool = %d, want default 1<<30", cfg.BufferPool)
	}
	// Reasoner sub-tree recurses across layers: baseurl from config.yml,
	// model from config.local.yml, device keeps the default.
	if cfg.Reasoner.BaseURL != "http://cfg.test:11435/v1" {
		t.Fatalf("reasoner.baseurl = %q, want base value", cfg.Reasoner.BaseURL)
	}
	if cfg.Reasoner.Model != "local-model" {
		t.Fatalf("reasoner.model = %q, want local-model (config.local.yml)", cfg.Reasoner.Model)
	}
	if cfg.Reasoner.Device != "cpu" {
		t.Fatalf("reasoner.device = %q, want default cpu", cfg.Reasoner.Device)
	}
	// Incubator: import list from config.yml, container keeps the default.
	if len(cfg.Incubator.Imports) != 1 {
		t.Fatalf("incubator.imports = %+v, want 1 import", cfg.Incubator.Imports)
	}
	imp := cfg.Incubator.Imports[0]
	if imp.Label != "archive" || imp.User != "owner@example.com" || imp.Source != "var/corpus/mail/archive" {
		t.Fatalf("incubator import wrong: %+v", imp)
	}
	if cfg.Incubator.Container != "mailserver" {
		t.Fatalf("incubator.container = %q, want default mailserver", cfg.Incubator.Container)
	}
}

// TestLoad_ProcessEnvOverridesLayer checks that process env (legacy names)
// wins over every lower layer.
func TestLoad_ProcessEnvOverridesLayer(t *testing.T) {
	t.Setenv("KB_PORT", "9999")
	t.Setenv("BRAIN_SEARCH_URL", "https://override.example.com")
	cfg := loadTest(t, filepath.Join("testdata", "basic"))

	if cfg.Port != 9999 {
		t.Fatalf("port = %d, want 9999 (KB_PORT)", cfg.Port)
	}
	if cfg.Search.URL != "https://override.example.com" {
		t.Fatalf("search.url = %q, want process env override", cfg.Search.URL)
	}
}

// TestLoad_KeyNormalizationLowerAlnum checks that keys with mixed case or
// non-alnum separators normalize to the typed field (Buffer-Pool → bufferpool,
// Search-Cmd → searchcmd, Root → root).
func TestLoad_KeyNormalizationLowerAlnum(t *testing.T) {
	isolateEnv(t)
	cfg := loadTest(t, filepath.Join("testdata", "normalize"))

	if cfg.BufferPool != 2048 {
		t.Fatalf("buffer_pool = %d, want 2048 (Buffer-Pool normalized)", cfg.BufferPool)
	}
	if cfg.SearchCmd != "var/bin/brain-search" {
		t.Fatalf("search_cmd = %q, want var/bin/brain-search (Search-Cmd normalized)", cfg.SearchCmd)
	}
	if cfg.Root != "/var/lib/2dph" {
		t.Fatalf("root = %q, want /var/lib/2dph (Root normalized)", cfg.Root)
	}
}

// TestLegacyEnvMapping maps every transitional KB_* / BRAIN_SEARCH_* env var
// onto the corresponding typed Config field.
func TestLegacyEnvMapping(t *testing.T) {
	t.Setenv("KB_ROOT", "/srv/r")
	t.Setenv("KB_BUFFER_POOL", "1073741824")
	t.Setenv("KBSEARCH_MODEL", "models/potion")
	t.Setenv("KBSEARCH_PORT", "18000")
	t.Setenv("KBSEARCH_NO_DAEMON", "1")
	t.Setenv("KBTEST_EPS", "0.01")
	t.Setenv("KB_WORKERS", "8")
	t.Setenv("KB_HOST", "0.0.0.0")
	t.Setenv("KB_PPROF", "6060")
	t.Setenv("KB_SEARCH_CMD", "/opt/search/bin")
	t.Setenv("KB_INDEX_ALLOW_LIVE", "1")
	t.Setenv("KB_WATCH_INTERVAL", "45")
	t.Setenv("KB_WATCH_DIRS", "/var/corpus /var/mail")
	t.Setenv("OO_CLI", "/opt/bin/oo")
	t.Setenv("HF_HOME", "/opt/hf")
	t.Setenv("BRAIN_SEARCH_URL", "https://sx.example.com")
	t.Setenv("BRAIN_SEARCH_USER", "svc")
	t.Setenv("BRAIN_SEARCH_PASS", "s3cret")
	t.Setenv("BRAIN_SEARCH_CACHE", "/opt/cache.db")
	t.Setenv("BRAIN_SEARCH_ENV", "/opt/search.env")
	t.Setenv("REASONER_BASE_URL", "http://r.test:11435/v1")
	t.Setenv("REASONER_MODEL", "r-model")
	t.Setenv("REASONER_DEVICE", "gpu")

	cfg := loadTest(t, t.TempDir()) // no files: defaults + legacy env

	if cfg.Root != "/srv/r" {
		t.Errorf("root = %q, want /srv/r", cfg.Root)
	}
	if cfg.BufferPool != 1073741824 {
		t.Errorf("buffer_pool = %d, want 1073741824", cfg.BufferPool)
	}
	if cfg.Model != "models/potion" {
		t.Errorf("model = %q, want models/potion", cfg.Model)
	}
	if cfg.SearchDaemonPort != 18000 {
		t.Errorf("search_daemon_port = %d, want 18000", cfg.SearchDaemonPort)
	}
	if !cfg.SearchNoDaemon {
		t.Error("search_no_daemon = false, want true")
	}
	if cfg.Eps != "0.01" {
		t.Errorf("eps = %q, want 0.01", cfg.Eps)
	}
	if cfg.Workers != 8 {
		t.Errorf("workers = %d, want 8", cfg.Workers)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("host = %q, want 0.0.0.0", cfg.Host)
	}
	if cfg.Pprof != "6060" {
		t.Errorf("pprof = %q, want 6060", cfg.Pprof)
	}
	if cfg.SearchCmd != "/opt/search/bin" {
		t.Errorf("search_cmd = %q, want /opt/search/bin", cfg.SearchCmd)
	}
	if !cfg.IndexAllowLive {
		t.Error("index_allow_live = false, want true")
	}
	if cfg.WatchInterval != 45 {
		t.Errorf("watch_interval = %d, want 45", cfg.WatchInterval)
	}
	if len(cfg.WatchDirs) != 2 || cfg.WatchDirs[0] != "/var/corpus" || cfg.WatchDirs[1] != "/var/mail" {
		t.Errorf("watch_dirs = %v, want [/var/corpus /var/mail]", cfg.WatchDirs)
	}
	if cfg.OOCLI != "/opt/bin/oo" {
		t.Errorf("oocli = %q, want /opt/bin/oo", cfg.OOCLI)
	}
	if cfg.HFHome != "/opt/hf" {
		t.Errorf("hf_home = %q, want /opt/hf", cfg.HFHome)
	}
	if cfg.Search.URL != "https://sx.example.com" {
		t.Errorf("search.url = %q", cfg.Search.URL)
	}
	if cfg.Search.User != "svc" {
		t.Errorf("search.user = %q", cfg.Search.User)
	}
	if cfg.Search.Pass != "s3cret" {
		t.Errorf("search.pass = %q", cfg.Search.Pass)
	}
	if cfg.Search.Cache != "/opt/cache.db" {
		t.Errorf("search.cache = %q", cfg.Search.Cache)
	}
	if cfg.Search.Env != "/opt/search.env" {
		t.Errorf("search.env = %q, want /opt/search.env", cfg.Search.Env)
	}
	if cfg.Reasoner.BaseURL != "http://r.test:11435/v1" {
		t.Errorf("reasoner.baseurl = %q, want legacy env override", cfg.Reasoner.BaseURL)
	}
	if cfg.Reasoner.Model != "r-model" {
		t.Errorf("reasoner.model = %q, want legacy env override", cfg.Reasoner.Model)
	}
	if cfg.Reasoner.Device != "gpu" {
		t.Errorf("reasoner.device = %q, want legacy env override", cfg.Reasoner.Device)
	}
}

func TestLoad_MissingLayersYieldDefaults(t *testing.T) {
	isolateEnv(t)
	cfg := loadTest(t, t.TempDir()) // no config files at all
	if cfg.Port != 8630 {
		t.Fatalf("port = %d, want default 8630", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("host = %q, want default 127.0.0.1", cfg.Host)
	}
	if cfg.Workers != 4 {
		t.Fatalf("workers = %d, want default 4", cfg.Workers)
	}
}

func TestMergeMaps_RecurseAndLastWriteWins(t *testing.T) {
	dst := map[string]any{"a": map[string]any{"x": "1", "y": "0"}, "n": 1}
	src := map[string]any{"a": map[string]any{"y": "2", "z": "3"}, "n": 2}
	mergeMaps(dst, src)
	if dst["n"] != 2 {
		t.Fatalf("scalar n = %v, want 2 (last-write-wins)", dst["n"])
	}
	a := dst["a"].(map[string]any)
	if a["x"] != "1" {
		t.Fatalf("a.x = %v, want 1 (recurse keeps dst-only key)", a["x"])
	}
	if a["y"] != "2" {
		t.Fatalf("a.y = %v, want 2 (src overrides)", a["y"])
	}
	if a["z"] != "3" {
		t.Fatalf("a.z = %v, want 3 (src-only key)", a["z"])
	}
}

// TestLoad_PSTSection checks the pst.* section round-trips: the sources list
// (label+path pairs), the readpst binary override and the out/state path
// overrides. Fixture paths are synthetic and relative (no host paths).
func TestLoad_PSTSection(t *testing.T) {
	isolateEnv(t)
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "etc", "brain")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yml := `root: "` + filepath.ToSlash(dir) + `"
pst:
  sources:
    - label: pst-archive
      path: fixtures/archive.pst
    - label: pst-backup-main
      path: fixtures/backup-main.pst
  readpst: var/dist/readpst/usr/bin/readpst
  out: var/corpus/mail/pst
  state: var/state/pst.json
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := loadTest(t, dir)
	if len(cfg.PST.Sources) != 2 {
		t.Fatalf("pst.sources = %d entries, want 2", len(cfg.PST.Sources))
	}
	if cfg.PST.Sources[0].Label != "pst-archive" || cfg.PST.Sources[0].Path != "fixtures/archive.pst" {
		t.Errorf("pst.sources[0] = %+v", cfg.PST.Sources[0])
	}
	if cfg.PST.Sources[1].Label != "pst-backup-main" {
		t.Errorf("pst.sources[1].label = %q", cfg.PST.Sources[1].Label)
	}
	if cfg.PST.ReadPST != "var/dist/readpst/usr/bin/readpst" {
		t.Errorf("pst.readpst = %q", cfg.PST.ReadPST)
	}
	if cfg.PST.Out != "var/corpus/mail/pst" || cfg.PST.State != "var/state/pst.json" {
		t.Errorf("pst out/state = %q/%q", cfg.PST.Out, cfg.PST.State)
	}
}

func TestDefaults(t *testing.T) {
	isolateEnv(t)
	d := Defaults()
	if d.Port != 8630 || d.Workers != 4 || d.Host != "127.0.0.1" ||
		d.BufferPool != 1<<30 || d.SearchDaemonPort != 17830 || d.WatchInterval != 30 {
		t.Fatalf("unexpected defaults: %+v", d)
	}
	if d.Reasoner.BaseURL != "http://127.0.0.1:11435/v1" ||
		d.Reasoner.Model != "qwen3.5:9b" || d.Reasoner.Device != "cpu" {
		t.Fatalf("unexpected reasoner defaults: %+v", d.Reasoner)
	}
	// ANN is on by default (issue #206): serve/CLI search via the index,
	// falling back to the linear scan when the index is missing/corrupt.
	if !d.Vector.ANN.Enabled {
		t.Fatal("vector.ann.enabled must default to true (#206)")
	}
}

// TestLoad_VectorANNEnv maps the vector.ann env names (underscore-split into
// nested keys) onto the typed ANN config: enabled/nprobe overrides and the
// defaults for the fields the env does not touch.
func TestLoad_VectorANNEnv(t *testing.T) {
	isolateEnv(t)
	t.Setenv("VECTOR_ANN_ENABLED", "true")
	t.Setenv("VECTOR_ANN_NPROBE", "256")
	cfg := loadTest(t, t.TempDir()) // no files: defaults + env
	if !cfg.Vector.ANN.Enabled {
		t.Fatal("vector.ann.enabled = false, want true from env")
	}
	if cfg.Vector.ANN.NProbe != 256 {
		t.Fatalf("vector.ann.nprobe = %d, want 256 from env", cfg.Vector.ANN.NProbe)
	}
	if cfg.Vector.ANN.Dim != 0 {
		t.Fatalf("vector.ann.dim = %d, want 0 (untouched default)", cfg.Vector.ANN.Dim)
	}
}
