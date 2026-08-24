//go:build cgo && system_ladybug

package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	lbug "github.com/LadybugDB/go-ladybug"
)

var (
	db   *lbug.Database
	conn *lbug.Connection
)

// brainMu guards the package-global db/conn pair. Serve endpoints that run
// on the long-lived read handle (Search/Get/Stats/Audit and the search /
// read helpers) hold it as RLock for the whole read; the Ingest write window
// closes and reopens the pair under Lock, so a handler can never touch a
// connection that a concurrent ingest just closed (C use-after-close).
var brainMu sync.RWMutex

// RepoRoot returns KB_ROOT or walks up from the executable / cwd.
func RepoRoot() string {
	return repoRoot()
}

func repoRoot() string {
	if v := brainCfg().Root; v != "" {
		return v
	}
	if wd, err := os.Getwd(); err == nil {
		if root := findRepoRoot(wd); root != "" {
			return root
		}
	}
	self, err := os.Executable()
	if err == nil {
		if root := findRepoRoot(filepath.Dir(self)); root != "" {
			return root
		}
	}
	return "."
}

func findRepoRoot(dir string) string {
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "var")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// dbPathFn resolves the brain DB file. A var so tests can point the whole
// read+write stack at a temp fixture without touching the production kb.lbug.
var dbPathFn = realDBPath

func dbPath() string { return dbPathFn() }

func realDBPath() string {
	return filepath.Join(repoRoot(), "var", "kb.lbug")
}

func openBrain() error {
	brainMu.Lock()
	defer brainMu.Unlock()
	return openWithSandboxLocked(brainCfg().Eps)
}

// openWithSandbox opens the long-lived serve read connection (write path
// excluded). Callers outside the write window take brainMu.Lock.
func openWithSandbox(epsv string) error {
	brainMu.Lock()
	defer brainMu.Unlock()
	return openWithSandboxLocked(epsv)
}

// openWithSandboxLocked assumes brainMu is held write-locked.
func openWithSandboxLocked(epsv string) error {
	cfg := lbug.DefaultSystemConfig()
	cfg.MaxNumThreads = 8
	pool := int64(1 << 30) // 1GB
	if v := brainCfg().BufferPool; v > 0 {
		pool = v
	}
	cfg.BufferPoolSize = uint64(pool)

	var err error
	db, err = lbug.OpenDatabase(dbPath(), cfg)
	if err != nil {
		return fmt.Errorf("OpenDatabase: %w", err)
	}

	conn, err = lbug.OpenConnection(db)
	if err != nil {
		closeBrainLocked()
		return fmt.Errorf("OpenConnection: %w", err)
	}
	// Session settings need a live connection; running this before
	// OpenConnection dereferenced a nil *Connection.
	if epsv != "" {
		if strings.ContainsAny(epsv, "'\\") {
			closeBrainLocked()
			return fmt.Errorf("SET STREAM_SANDBOX: invalid value")
		}
		if res, err := conn.Query("SET STREAM_SANDBOX = '" + epsv + "'"); err != nil {
			qClose(res)
			closeBrainLocked()
			return fmt.Errorf("SET STREAM_SANDBOX: %w", err)
		} else {
			qClose(res)
		}
	}
	for _, ext := range []string{"FTS", "VECTOR"} {
		if err := loadExt(conn, ext); err != nil {
			closeBrainLocked()
			return fmt.Errorf("serve handle %w", err)
		}
	}
	migrateIntervalColumns()
	return nil
}

// qClose destroys a QueryResult's C buffers even when the result set is empty.
func qClose(res *lbug.QueryResult) {
	if res != nil {
		res.Close()
	}
}

// migrateIntervalColumns adds D24 valid_from/valid_to on existing Leaf tables.
// Fresh CREATE already has them; ALTER is a no-op when the column exists.
func migrateIntervalColumns() {
	for _, col := range []string{"valid_from", "valid_to"} {
		res, err := conn.Query("ALTER TABLE Leaf ADD " + col + " STRING")
		qClose(res)
		_ = err
	}
}

// closeBrain releases the serve read handle. Callers outside the write
// window take brainMu.Lock.
func closeBrain() {
	brainMu.Lock()
	defer brainMu.Unlock()
	closeBrainLocked()
}

// closeBrainLocked assumes brainMu is held write-locked.
func closeBrainLocked() {
	if conn != nil {
		conn.Close()
		conn = nil
	}
	if db != nil {
		db.Close()
		db = nil
	}
}
