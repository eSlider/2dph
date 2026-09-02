package incubator

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Entry records one imported message: the canonical dedup key (Message-ID
// canon or "body-sha256:<hex>"), its source path and the target mailbox.
type Entry struct {
	Key     string `json:"key"`
	Path    string `json:"path"`
	Mailbox string `json:"mailbox"`
}

// Manifest is the idempotency state of one incubator source
// (var/state/incubator-<label>.json, issue #252 п.2): the source of truth for
// "already imported". It is gitignored runtime state, never committed.
type Manifest struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

const manifestVersion = 1

// Has reports whether the canonical key was already imported.
func (m *Manifest) Has(key string) bool {
	for _, e := range m.Entries {
		if e.Key == key {
			return true
		}
	}
	return false
}

// Add appends one entry and keeps the manifest deterministic (sorted by key).
func (m *Manifest) Add(key, path, mailbox string) {
	m.Entries = append(m.Entries, Entry{Key: key, Path: path, Mailbox: mailbox})
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].Key < m.Entries[j].Key })
}

// LoadManifest reads the state file; a missing file is a first run (empty
// manifest). A corrupt file is an error — never a silent reset.
func LoadManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{Version: manifestVersion}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("incubator: corrupt manifest %s: %w", path, err)
	}
	return m, nil
}

// Save writes the manifest atomically (temp file + fsync + rename), so a
// reader never observes a partial state file.
func (m *Manifest) Save(path string) error {
	m.Version = manifestVersion
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
