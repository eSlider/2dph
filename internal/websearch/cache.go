package websearch

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const cacheSchema = `
CREATE TABLE IF NOT EXISTS responses (
  key     TEXT PRIMARY KEY,
  fetched REAL NOT NULL,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value REAL NOT NULL
);
`

type Cache struct {
	db *sql.DB
}

func OpenCache(path string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(cacheSchema); err != nil {
		db.Close()
		return nil, err
	}
	return &Cache{db: db}, nil
}

func (c *Cache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

func (c *Cache) Get(key string, ttl, now float64) (*Payload, error) {
	var fetched float64
	var raw string
	err := c.db.QueryRow("SELECT fetched, payload FROM responses WHERE key = ?", key).Scan(&fetched, &raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if now-fetched > ttl {
		return nil, nil
	}
	var p Payload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (c *Cache) Put(key string, p Payload, now float64) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = c.db.Exec(
		"INSERT OR REPLACE INTO responses (key, fetched, payload) VALUES (?, ?, ?)",
		key, now, string(raw),
	)
	return err
}

func (c *Cache) LastCall() (*float64, error) {
	var v float64
	err := c.db.QueryRow("SELECT value FROM meta WHERE key = 'last_call'").Scan(&v)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (c *Cache) MarkCall(now float64) error {
	_, err := c.db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES ('last_call', ?)", now)
	return err
}
