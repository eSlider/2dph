package docgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	sourcePrefix  = "source="
	channelPrefix = "channel="
)

// Partition — пара source/channel канона kind=document (hive-партиция).
type Partition struct {
	Source  string
	Channel string
}

// String — "source/channel" для отчётов и логов.
func (p Partition) String() string { return p.Source + "/" + p.Channel }

// Partitions перечисляет source/channel kind=document по hive-каталогу:
// <hive>/source=<source>/channel=<channel>/dt=*/*.parquet. На практике источник
// один — source=portals, вендор — в channel=<vendor>; список всё равно
// выводится из фактических партиций (единый реестр — сам канон), НЕ
// хардкодится. Отсутствие hive (дерево ещё не собрано gator'ом) — не ошибка:
// пустой список.
func Partitions(hiveRoot string) ([]Partition, error) {
	if hiveRoot == "" {
		return nil, fmt.Errorf("gator document hive root is empty")
	}
	srcEntries, err := os.ReadDir(hiveRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read gator document hive %s: %w", hiveRoot, err)
	}
	var out []Partition
	for _, se := range srcEntries {
		if !se.IsDir() || !strings.HasPrefix(se.Name(), sourcePrefix) {
			continue
		}
		src := strings.TrimPrefix(se.Name(), sourcePrefix)
		if src == "" {
			continue
		}
		chEntries, err := os.ReadDir(filepath.Join(hiveRoot, se.Name()))
		if err != nil {
			return nil, fmt.Errorf("read gator document source %s: %w", se.Name(), err)
		}
		for _, ce := range chEntries {
			if !ce.IsDir() || !strings.HasPrefix(ce.Name(), channelPrefix) {
				continue
			}
			ch := strings.TrimPrefix(ce.Name(), channelPrefix)
			if ch == "" {
				continue
			}
			out = append(out, Partition{Source: src, Channel: ch})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Channel < out[j].Channel
	})
	return out, nil
}

// HasParquet reports whether the hive glob matches at least one parquet file.
// Empty partitions (dt dirs with no packed files yet) must be skipped: DuckDB
// read_parquet errors on a glob with no matches.
func HasParquet(glob string) bool {
	matches, err := filepath.Glob(glob)
	return err == nil && len(matches) > 0
}

// PackMtime — самая свежая mtime parquet-партиции канона kind=document:
// признак свежести проекции gator (после pack). Нет файлов/нет hive → 0.
func PackMtime(hiveRoot string) (int64, error) {
	if hiveRoot == "" {
		return 0, nil
	}
	var newest int64
	err := filepath.WalkDir(hiveRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".parquet") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if m := info.ModTime().UnixNano(); m > newest {
			newest = m
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newest, nil
}
