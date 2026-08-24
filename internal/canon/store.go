package canon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eSlider/2dph/pkg/utils"
)

// Manifest is the dedup ledger: message id -> sha256 of its canonical JSON body.
// A sync wave skips any message whose hash is unchanged (upsert idempotency,
// dialog-package pattern: last-write-wins only on actual change).
type Manifest struct {
	Messages map[string]string `json:"messages"`
}

// Empty reports whether the manifest holds no entries.
func (m Manifest) Empty() bool { return len(m.Messages) == 0 }

// Hash returns the recorded body hash for id and whether it exists.
func (m Manifest) Hash(id string) (string, bool) {
	h, ok := m.Messages[id]
	return h, ok
}

// Store persists canonical messages as JSON under
// root/<platform>/<thread>/<id>.json plus root/manifest.json. Layout and path
// segments are sanitized so no caller-supplied id can escape the corpus root.
type Store struct {
	root string
}

// NewStore returns a Store rooted at root (var/corpus/mail or var/corpus/chats).
func NewStore(root string) *Store { return &Store{root: root} }

// Root returns the corpus root directory.
func (s *Store) Root() string { return s.root }

var errNoChange = errors.New("canon: message unchanged, not rewritten")

// Write persists msg if its body hash differs from the manifest (idempotent).
// It returns (true, nil) when the message was (re)written and (false, nil) when
// it was skipped as unchanged.
func (s *Store) Write(ctx context.Context, msg Message) (bool, error) {
	body, err := json.Marshal(msg)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	mf, err := s.LoadManifest(ctx)
	if err != nil {
		return false, err
	}
	if mf.Messages == nil {
		mf.Messages = map[string]string{}
	}
	if cur, ok := mf.Messages[msg.ID]; ok && cur == hash {
		return false, nil
	}

	path := s.messagePath(msg)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return false, err
	}

	mf.Messages[msg.ID] = hash
	if err := s.SaveManifest(ctx, mf); err != nil {
		return false, err
	}
	return true, nil
}

// Read returns every canonical message stored under root, sorted by id for a
// deterministic order. Unreadable/corrupt files are skipped rather than killing
// a sync wave.
func (s *Store) Read(ctx context.Context) ([]Message, error) {
	var out []Message
	err := filepath.WalkDir(s.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // keep walking; a bad subtree must not fail the wave
		}
		if d.IsDir() || d.Name() == manifestName {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		var m Message
		if json.Unmarshal(data, &m) != nil {
			return nil
		}
		out = append(out, m)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

const manifestName = "manifest.json"

// LoadManifest reads the manifest, returning an empty one when it does not exist
// yet. A corrupt manifest is treated as empty so storage recovers.
func (s *Store) LoadManifest(ctx context.Context) (Manifest, error) {
	var mf Manifest
	data, err := os.ReadFile(s.manifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Manifest{Messages: map[string]string{}}, nil
		}
		return Manifest{Messages: map[string]string{}}, nil
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		return Manifest{Messages: map[string]string{}}, nil
	}
	if mf.Messages == nil {
		mf.Messages = map[string]string{}
	}
	return mf, nil
}

// SaveManifest writes the manifest atomically.
func (s *Store) SaveManifest(ctx context.Context, mf Manifest) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return err
	}
	tmp := s.manifestPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.manifestPath())
}

// messagePath is root/<platform>/<thread>/<id>.json. Empty thread becomes the
// literal "nothread" so messages still nest in a folder.
func (s *Store) messagePath(m Message) string {
	platform := utils.SafeSegment(m.Platform)
	if platform == "" {
		platform = "unknown"
	}
	thread := utils.SafeSegment(m.ThreadID)
	if thread == "" {
		thread = "nothread"
	}
	return filepath.Join(s.root, platform, thread, utils.SafeSegment(m.ID)+".json")
}

func (s *Store) manifestPath() string { return filepath.Join(s.root, manifestName) }
