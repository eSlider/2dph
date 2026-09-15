package docgraph

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// strValue нормализует скаляр колонки duckdb (string/[]byte) в строку.
func strValue(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	case nil:
		return ""
	default:
		return fmt.Sprint(s)
	}
}

// timeValue принимает TIMESTAMP/DATE-колонку как time.Time (duckdb) или
// строку: RFC3339 (parquet VARCHAR, как пишет gator pack), "2006-01-02
// 15:04:05" или date-only "2006-01-02" (hive-партиция dt=).
func timeValue(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case nil:
		return time.Time{}, nil
	}
	s := strings.TrimSpace(strValue(v))
	if s == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if tm, err := time.Parse(layout, s); err == nil {
			return tm, nil
		}
	}
	return time.Time{}, fmt.Errorf("docgraph: time %q: unrecognized format", s)
}

// intValue принимает BIGINT (int64/int32) или числовую строку.
func intValue(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int32:
		return int64(n)
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case nil:
		return 0
	default:
		i, err := strconv.ParseInt(strings.TrimSpace(strValue(v)), 10, 64)
		if err != nil {
			return 0
		}
		return i
	}
}

// MapRow конвертирует один row kind=document (JSON-shape: map[string]any, как
// отдаёт pkg/duckdb.QueryRows) в типизированный Row по канону
// query.DocumentsColumns (gator PR #140). Документ без identity не ошибка
// маппера: он пропускается на записи (RowToLeaf).
func MapRow(m map[string]any) (Row, error) {
	var r Row
	r.Source = strings.TrimSpace(strValue(m["source"]))
	r.Channel = strings.TrimSpace(strValue(m["channel"]))
	r.ContentHash = strings.TrimSpace(strValue(m["content_hash"]))
	r.ExternalID = strings.TrimSpace(strValue(m["external_id"]))
	r.Title = strValue(m["title"])
	r.Filename = strings.TrimSpace(strValue(m["filename"]))
	r.MIME = strings.TrimSpace(strValue(m["mime"]))
	r.SHA256 = strings.TrimSpace(strValue(m["sha256"]))
	r.OOPath = strings.TrimSpace(strValue(m["oo_path"]))
	r.Ref = strings.TrimSpace(strValue(m["ref"]))
	r.Size = intValue(m["size"])
	var err error
	if r.Date, err = timeValue(m["dt"]); err != nil {
		return Row{}, err
	}
	if r.PostedAt, err = timeValue(m["posted_at"]); err != nil {
		return Row{}, err
	}
	if r.ObservedAt, err = timeValue(m["observed_at"]); err != nil {
		return Row{}, err
	}
	if r.DocDate, err = timeValue(m["doc_date"]); err != nil {
		return Row{}, err
	}
	return r, nil
}

// MapRows конвертирует пачку rows; строки с ошибкой маппинга пропускаются
// (битая строка не валит весь импорт), счётчик skipped и первая ошибка
// возвращаются для dry-run отчёта.
func MapRows(raw []map[string]any) (rows []Row, skipped int, firstErr error) {
	rows = make([]Row, 0, len(raw))
	for _, m := range raw {
		r, err := MapRow(m)
		if err != nil {
			skipped++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		rows = append(rows, r)
	}
	return rows, skipped, firstErr
}
