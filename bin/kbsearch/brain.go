// Brain connection management using go-ladybug.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	lbug "github.com/LadybugDB/go-ladybug"
)

var (
	db   *lbug.Database
	conn *lbug.Connection
)

func repoRoot() string {
	// Try KB_ROOT env, then walk up from binary
	if v := os.Getenv("KB_ROOT"); v != "" {
		return v
	}
	self, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(self)
		for i := 0; i < 5; i++ {
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
	}
	return "."
}

func dbPath() string {
	return filepath.Join(repoRoot(), "var", "kb.lbug")
}

func openBrain() error {
	return openWithOpts(2, eps())
}

func openWithOpts(allow int, epsv string) error {
	cfg := lbug.DefaultSystemConfig()
	cfg.MaxNumThreads = 8
	cfg.BufferPoolSize = 1 << 30 // 1GB

	var err error
	db, err = lbug.OpenDatabase(dbPath(), cfg)
	if err != nil {
		return fmt.Errorf("OpenDatabase: %w", err)
	}
	if epsv != "" {
		if _, err := conn.Query("SET STREAM_SANDBOX = '" + epsv + "'"); err != nil {
			return err
		}
	}

	conn, err = lbug.OpenConnection(db)
	if err != nil {
		return fmt.Errorf("OpenConnection: %w", err)
	}
	if _, err := conn.Query("LOAD EXTENSION FTS"); err != nil {
		return fmt.Errorf("LOAD EXTENSION FTS: %w", err)
	}
	if _, err := conn.Query("LOAD EXTENSION VECTOR"); err != nil {
		return fmt.Errorf("LOAD EXTENSION VECTOR: %w", err)
	}
	return nil
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