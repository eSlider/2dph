//go:build cgo && system_ladybug

package brain

// Write-путь D-1.2 (#259): canon.Message → Ladybug-граф (Person/Message узлы
// + рёбра SENT/TO/CC/BCC/REPLY_TO). Идемпотентность — MERGE по id (message_id
// / email), повторный sync не плодит дублей. Чистая деривация плана живёт в
// graphplan.go (cgo-free), здесь только исполнение через execParams.

import lbug "github.com/LadybugDB/go-ladybug"

// UpsertMessage MERGE-ит одно сообщение и его граф.
func UpsertMessage(conn *lbug.Connection, in MessageInput) error {
	return UpsertMessages(conn, []MessageInput{in})
}

// UpsertMessages пишет пачку сообщений одной транзакцией. Person name-мердж:
// последнее сообщение в пачке (и позднейший повторный sync) перезаписывает
// name при том же email — SET в MERGE, дублей узлов нет.
func UpsertMessages(conn *lbug.Connection, msgs []MessageInput) error {
	if len(msgs) == 0 {
		return nil
	}
	started := false
	if res, err := conn.Query("BEGIN TRANSACTION"); err == nil {
		qClose(res)
		started = true
	} else {
		qClose(res)
	}
	for i := range msgs {
		if err := upsertMessage(conn, &msgs[i]); err != nil {
			if started {
				res, _ := conn.Query("ROLLBACK")
				qClose(res)
			}
			return err
		}
	}
	if started {
		if res, err := conn.Query("COMMIT"); err != nil {
			qClose(res)
			return err
		} else {
			qClose(res)
		}
	}
	return nil
}

// upsertMessage пишет одно сообщение: Person-узлы, Message-узел, рёбра.
// REPLY_TO на ещё не импортированного родителя молча пропускается (MATCH не
// находит конец) — появляется при повторном прогоне после импорта родителя.
func upsertMessage(conn *lbug.Connection, in *MessageInput) error {
	persons, edges := planGraph(*in)
	for _, p := range persons {
		if err := execParams(conn, personUpsertQuery(), map[string]any{
			"id": p.ID, "name": p.Name, "email": p.Email,
		}); err != nil {
			return err
		}
	}
	if in.ID == "" {
		return nil // сообщение без id узлом не становится (рёбра бессмысленны)
	}
	args := map[string]any{
		"id":      in.ID,
		"tid":     in.ThreadID,
		"folder":  trimField(in.Folder),
		"subject": in.Subject,
		"sent":    sentAtText(in.SentAt),
		"gref":    trimField(in.GatorRef),
		"body":    in.Body,
	}
	if err := execParams(conn, messageUpsertQuery(), args); err != nil {
		return err
	}
	for _, e := range edges {
		es, ok := edgeSchema(e.Type)
		if !ok {
			continue // PART_OF и пр. на этом этапе не пишем
		}
		q := "MATCH (a:" + es.fromLabel + " {id:$from}), (b:" + es.toLabel + " {id:$to})" +
			" MERGE (a)-[:" + es.rel + "]->(b)"
		if err := execParams(conn, q, map[string]any{"from": e.From, "to": e.To}); err != nil {
			return err
		}
	}
	return nil
}
