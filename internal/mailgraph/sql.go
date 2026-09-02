package mailgraph

// SQL read-запроса канала kind=mail. Значения экранируются (QuoteLiteral),
// имена колонок фиксированы — пользовательский ввод в SQL не попадает.

import "strings"

// mailColumns — read-колонки коннектора (read-схема gator query.Mail, #89),
// "from"/"date" — зарезервированные слова DuckDB → цитируются.
const mailColumns = `"message_id", "folder", "from", "to", "cc", "bcc", "in_reply_to", "subject", "date", "body", "content_hash"`

// quoteLit экранирует литерал: одинарная кавычка удваивается.
func quoteLit(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ReadSQL собирает SELECT канала kind=mail поверх read_parquet(glob,
// hive_partitioning=true): latest-версия на message_id (row_number по
// observed_at DESC, G-8.0), письма с tombstone-latest (deleted=true)
// исключаются (G-9.3), channel фильтруется WHERE. glob — путь к hive
// parquet/mail (source=mail/channel=*/dt=*/*.parquet).
func ReadSQL(glob, channel string) string {
	return `SELECT ` + mailColumns + ` FROM (` +
		`SELECT ` + mailColumns + `, "deleted", row_number() OVER (PARTITION BY "message_id" ORDER BY "observed_at" DESC) AS __rn ` +
		`FROM read_parquet(` + quoteLit(glob) + `, hive_partitioning=true) ` +
		`WHERE "channel" = ` + quoteLit(channel) + `) ` +
		`WHERE __rn = 1 AND NOT COALESCE("deleted", false)`
}
