// Freshness state for the gator→graph→index cycle (issue #292).
//
// The periodic compose service writes a small YAML state file after each
// successful cycle; the read contract (/stats, scripts/stack/status) reads it
// back and compares it with the kb.lbug mtime and the newest gator parquet
// pack. Cgo-free so the read path unit-tests without the Ladybug library.
package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultStaleAfter is the fallback staleness horizon when the state file
// does not carry one: a cycle is expected well inside a day, so a day plus
// slack flags a silently broken loop.
const DefaultStaleAfter = 26 * time.Hour

// Freshness is the persisted last-good state of the import/index cycle.
// Times are RFC3339 UTC strings, empty when that stage never ran.
type Freshness struct {
	ImportAt   string   `yaml:"import_at,omitempty"`
	IndexAt    string   `yaml:"index_at,omitempty"`
	KBMtime    string   `yaml:"kb_mtime,omitempty"`
	PackMtime  string   `yaml:"pack_mtime,omitempty"`
	Channels   []string `yaml:"channels,omitempty"`
	StaleAfter string   `yaml:"stale_after,omitempty"`
	LastError  string   `yaml:"last_error,omitempty"`
	UpdatedAt  string   `yaml:"updated_at,omitempty"`
}

// FreshnessView is the derived read-contract shape (state + live kb mtime).
type FreshnessView struct {
	IndexAt    string `json:"index_at" yaml:"index_at"`
	ImportAt   string `json:"import_at" yaml:"import_at"`
	KBMtime    string `json:"kb_mtime" yaml:"kb_mtime"`
	PackMtime  string `json:"pack_mtime" yaml:"pack_mtime"`
	StaleAfter string `json:"stale_after" yaml:"stale_after"`
	Stale      bool   `json:"stale" yaml:"stale"`
	LastError  string `json:"last_error,omitempty" yaml:"last_error,omitempty"`
}

// FreshnessPath resolves the state file under a repo root.
func FreshnessPath(root string) string {
	return filepath.Join(root, "var", "state", "index-freshness.yml")
}

// LoadFreshness reads the state file; a missing or malformed file yields the
// zero state (staleness is then reported, never a hard error).
func LoadFreshness(root string) Freshness {
	var f Freshness
	b, err := os.ReadFile(FreshnessPath(root))
	if err != nil {
		return f
	}
	_ = yaml.Unmarshal(b, &f)
	return f
}

// SaveFreshness writes the state file atomically (tmp + rename) so a reader
// never sees a half-written file. YAML, not JSON (repo convention).
func SaveFreshness(root string, f Freshness) error {
	path := FreshnessPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("freshness dir: %w", err)
	}
	f.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	b, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("freshness marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("freshness write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("freshness rename: %w", err)
	}
	return nil
}

// ViewFreshnessForDB derives the read-contract view for a kb.lbug path. The
// repo root is the grandparent of the db (var/kb.lbug), where the state file
// and the pack mtime live.
func ViewFreshnessForDB(dbPath string) FreshnessView {
	root := filepath.Dir(filepath.Dir(dbPath))
	return ViewFreshness(root, dbPath)
}

// ViewFreshness merges the persisted state with the live kb.lbug mtime and
// reports staleness. kbPath empty defaults to <root>/var/kb.lbug.
func ViewFreshness(root, kbPath string) FreshnessView {
	f := LoadFreshness(root)
	if kbPath == "" {
		kbPath = filepath.Join(root, "var", "kb.lbug")
	}
	v := FreshnessView{
		ImportAt:   f.ImportAt,
		IndexAt:    f.IndexAt,
		PackMtime:  f.PackMtime,
		StaleAfter: f.StaleAfter,
		LastError:  f.LastError,
	}
	if m, err := os.Stat(kbPath); err == nil {
		v.KBMtime = m.ModTime().UTC().Format(time.RFC3339)
	}
	if v.StaleAfter == "" {
		v.StaleAfter = DefaultStaleAfter.String()
	}
	v.Stale = staleNow(v)
	return v
}

// staleNow decides staleness: an unknown index timestamp, an index older than
// the horizon, or a gator pack newer than the indexed kb all count as stale.
// A last_error from the cycle always wins (the loop is failing right now).
func staleNow(v FreshnessView) bool {
	if v.LastError != "" {
		return true
	}
	idx, ok := parseTime(v.IndexAt)
	if !ok {
		return true
	}
	if v.KBMtime == "" {
		return true // no readable kb: nothing is served fresh
	}
	horizon := DefaultStaleAfter
	if d, err := time.ParseDuration(v.StaleAfter); err == nil && d > 0 {
		horizon = d
	}
	if time.Since(idx) > horizon {
		return true
	}
	if pack, ok := parseTime(v.PackMtime); ok {
		if kb, ok := parseTime(v.KBMtime); ok && pack.After(kb) {
			return true
		}
	}
	return false
}

func parseTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
