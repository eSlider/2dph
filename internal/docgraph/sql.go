package docgraph

import "strings"

// SQL read-запроса kind=document. Значения экранируются (quoteLit), имена
// колонок фиксированы — пользовательский ввод в SQL не попадает.

// docColumns — read-колонки parquet/documents по канону gator PR #140
// (query.DocumentsColumns = "source, channel, dt, content_hash, posted_at,
// observed_at, external_id, title, doc_date, filename, mime, size, sha256,
// oo_path, ref"). source/channel/dt приходят из hive-партиции, остальные —
// из payload model.Document, вынесенного pack'ом наверх. Колонки цитируются:
// source/size/ref — имена, которые DuckDB склонен трактовать как
// ключевые/функции.
const docColumns = `"source", "channel", "dt", "content_hash", "posted_at", "observed_at", ` +
	`"external_id", "title", "doc_date", "filename", "mime", "size", "sha256", "oo_path", "ref"`

// quoteLit экранирует литерал: одинарная кавычка удваивается.
func quoteLit(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ReadSQL собирает SELECT документа поверх read_parquet(glob,
// hive_partitioning=true). glob — путь к hive parquet/documents
// (source=<s>/channel=<c>/dt=*/*.parquet); source/channel фильтруются WHERE.
// Latest-версия на external_id не вычисляется: Leaf.ContentHash дедуплицирует
// повтор одного документа при записи.
func ReadSQL(glob, source, channel string) string {
	return `SELECT ` + docColumns +
		` FROM read_parquet(` + quoteLit(glob) + `, hive_partitioning=true) ` +
		`WHERE "source" = ` + quoteLit(source) + ` AND "channel" = ` + quoteLit(channel)
}
