package mailgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// channelPrefix — партиция Hive для канала gator kind=mail.
const channelPrefix = "channel="

// Channels перечисляет каналы канона gator kind=mail по hive-каталогу:
// <hive>/source=mail/channel=<ch>/dt=*/*.parquet. Список выводится из
// фактических партиций (единый реестр — сам канон), НЕ хардкодится: новый
// канал в gator появляется в цикле import без правки 2dph. Отсутствие
// source=mail (дерево ещё не собрано gator'ом) — не ошибка: пустой список.
// Так периодический цикл продолжает работать, пока почта не произведена.
func Channels(hiveRoot string) ([]string, error) {
	if hiveRoot == "" {
		return nil, fmt.Errorf("gator mail hive root is empty")
	}
	src := filepath.Join(hiveRoot, "source=mail")
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read gator hive %s: %w", src, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), channelPrefix) {
			continue
		}
		ch := strings.TrimPrefix(e.Name(), channelPrefix)
		if ch == "" {
			continue
		}
		out = append(out, ch)
	}
	sort.Strings(out)
	return out, nil
}

// PackMtime — самая свежая mtime parquet-партиции канона (все каналы):
// признак свежести проекции gator (после pack). Пусто/нет файлов → zero time.
func PackMtime(hiveRoot string) (int64, error) {
	src := filepath.Join(hiveRoot, "source=mail")
	var newest int64
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
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
