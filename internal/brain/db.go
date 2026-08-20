//go:build cgo && system_ladybug

package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	lbug "github.com/LadybugDB/go-ladybug"
)

var (
	db   *lbug.Database
	conn *lbug.Connection
)

// RepoRoot returns KB_ROOT or walks up from the executable / cwd.
func RepoRoot() string {
	return repoRoot()
}

func repoRoot() string {
	if v := os.Getenv("KB_ROOT"); v != "" {
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

func dbPath() string {
	return filepath.Join(repoRoot(), "var", "kb.lbug")
}

func openBrain() error {
	return openWithSandbox(eps())
}

func openWithSandbox(epsv string) error {
	cfg := lbug.DefaultSystemConfig()
	cfg.MaxNumThreads = 8
	pool := int64(1 << 30) // 1GB
	if v := os.Getenv("KB_BUFFER_POOL"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			pool = n
		}
	}
	cfg.BufferPoolSize = uint64(pool)

	var err error
	db, err = lbug.OpenDatabase(dbPath(), cfg)
	if err != nil {
		return fmt.Errorf("OpenDatabase: %w", err)
	}

	conn, err = lbug.OpenConnection(db)
	if err != nil {
		closeBrain()
		return fmt.Errorf("OpenConnection: %w", err)
	}
	// Session settings need a live connection; running this before
	// OpenConnection dereferenced a nil *Connection.
	if epsv != "" {
		if strings.ContainsAny(epsv, "'\\") {
			closeBrain()
			return fmt.Errorf("SET STREAM_SANDBOX: invalid value")
		}
		if res, err := conn.Query("SET STREAM_SANDBOX = '" + epsv + "'"); err != nil {
			qClose(res)
			closeBrain()
			return fmt.Errorf("SET STREAM_SANDBOX: %w", err)
		} else {
			qClose(res)
		}
	}
	if res, err := conn.Query("LOAD EXTENSION FTS"); err != nil {
		qClose(res)
		closeBrain()
		return fmt.Errorf("LOAD EXTENSION FTS: %w", err)
	} else {
		qClose(res)
	}
	if res, err := conn.Query("LOAD EXTENSION VECTOR"); err != nil {
		qClose(res)
		closeBrain()
		return fmt.Errorf("LOAD EXTENSION VECTOR: %w", err)
	} else {
		qClose(res)
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

func closeBrain() {
	if conn != nil {
		conn.Close()
		conn = nil
	}
	if db != nil {
		db.Close()
		db = nil
	}
}
