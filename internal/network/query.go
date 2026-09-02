//go:build cgo && system_ladybug

package network

// Read-only запросы графа для сети связей (L-9.5 #234): Message/Person +
// рёбра SENT/TO/CC/BCC/REPLY_TO (D-1 #257) и Commit/Person/AUTHORED
// (L-9.4 #233) → строки Rows для BuildLinks. Ничего не пишет; читает весь
// mail+git слой графа (масштаб: 22k Message / 285 Commit — в память влезает,
// фильтрация по target/периоду — в чистой логике BuildLinks).
//
// Запросы намеренно простые (по одному типу ребра): Ladybug не отдаёт
// тип ребра функцией type() и не любит UNION — собираем роли в Go.

import (
	"fmt"

	lbug "github.com/LadybugDB/go-ladybug"
)

// LoadRows вычитывает mail+git слои графа в строки сети.
func LoadRows(conn *lbug.Connection) (Rows, error) {
	var rows Rows

	// Message-узлы: id/thread_id/sent_at/gator_ref.
	res, err := conn.Query(`MATCH (m:Message)
		RETURN m.id, m.thread_id, m.sent_at, m.gator_ref`)
	if err != nil {
		return rows, fmt.Errorf("network: message nodes: %w", err)
	}
	byID := map[string]int{}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			res.Close()
			return rows, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 4 {
			res.Close()
			return rows, fmt.Errorf("network: message row: %v %v", vals, err)
		}
		msg := MsgRow{
			ID:       fmt.Sprint(vals[0]),
			ThreadID: fmt.Sprint(vals[1]),
			SentAt:   fmt.Sprint(vals[2]),
			GatorRef: fmt.Sprint(vals[3]),
		}
		byID[msg.ID] = len(rows.Msgs)
		rows.Msgs = append(rows.Msgs, msg)
	}
	res.Close()

	// Рёбра письма. dir=true: Person→Message (sender), false: Message→Person.
	loadEdge := func(query, typ string, toMsg bool) error {
		res, err := conn.Query(query)
		if err != nil {
			return fmt.Errorf("network: edge %s: %w", typ, err)
		}
		defer res.Close()
		for res.HasNext() {
			row, err := res.Next()
			if err != nil {
				return err
			}
			vals, err := row.GetAsSlice()
			if err != nil || len(vals) < 2 {
				return fmt.Errorf("network: edge %s row: %v %v", typ, vals, err)
			}
			a, b := fmt.Sprint(vals[0]), fmt.Sprint(vals[1])
			var idx int
			var ok bool
			if toMsg { // a = Person, b = Message
				idx, ok = byID[b]
			} else { // a = Message, b = Person
				idx, ok = byID[a]
			}
			if !ok {
				continue // ребро к неизвестному узлу — пропуск
			}
			m := &rows.Msgs[idx]
			person := b
			if toMsg {
				m.Sender = a
				continue
			}
			switch typ {
			case "TO":
				m.To = append(m.To, person)
			case "CC":
				m.CC = append(m.CC, person)
			case "BCC":
				m.BCC = append(m.BCC, person)
			}
		}
		return nil
	}
	queries := []struct {
		q, typ string
		toMsg  bool
	}{
		{`MATCH (p:Person)-[:SENT]->(m:Message) RETURN p.id, m.id`, "SENT", true},
		{`MATCH (m:Message)-[:TO]->(p:Person) RETURN m.id, p.id`, "TO", false},
		{`MATCH (m:Message)-[:CC]->(p:Person) RETURN m.id, p.id`, "CC", false},
		{`MATCH (m:Message)-[:BCC]->(p:Person) RETURN m.id, p.id`, "BCC", false},
	}
	for _, q := range queries {
		if err := loadEdge(q.q, q.typ, q.toMsg); err != nil {
			return rows, err
		}
	}

	// REPLY_TO: письмо-ответ (a) на родителя (b): m(a).ReplyTo = b.id.
	res, err = conn.Query(`MATCH (a:Message)-[:REPLY_TO]->(b:Message) RETURN a.id, b.id`)
	if err != nil {
		return rows, fmt.Errorf("network: REPLY_TO: %w", err)
	}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			res.Close()
			return rows, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			res.Close()
			return rows, fmt.Errorf("network: REPLY_TO row: %v %v", vals, err)
		}
		if idx, ok := byID[fmt.Sprint(vals[0])]; ok {
			rows.Msgs[idx].ReplyTo = fmt.Sprint(vals[1])
		}
	}
	res.Close()

	// Person-узлы: id/name.
	res, err = conn.Query(`MATCH (p:Person) RETURN p.id, p.name`)
	if err != nil {
		return rows, fmt.Errorf("network: persons: %w", err)
	}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			res.Close()
			return rows, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			res.Close()
			return rows, fmt.Errorf("network: person row: %v %v", vals, err)
		}
		rows.Persons = append(rows.Persons, PersonRow{ID: fmt.Sprint(vals[0]), Name: fmt.Sprint(vals[1])})
	}
	res.Close()

	// Commit-узлы: id/repo/date + AUTHORED Person.email.
	res, err = conn.Query(`MATCH (c:Commit)-[:AUTHORED]->(p:Person)
		RETURN c.id, c.repo, c.date, p.id`)
	if err != nil {
		return rows, fmt.Errorf("network: commits: %w", err)
	}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			res.Close()
			return rows, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 4 {
			res.Close()
			return rows, fmt.Errorf("network: commit row: %v %v", vals, err)
		}
		rows.Commits = append(rows.Commits, CommitRow{
			ID:    fmt.Sprint(vals[0]),
			Repo:  fmt.Sprint(vals[1]),
			Date:  fmt.Sprint(vals[2]),
			Email: fmt.Sprint(vals[3]),
		})
	}
	res.Close()

	return rows, nil
}
