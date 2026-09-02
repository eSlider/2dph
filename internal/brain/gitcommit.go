//go:build cgo && system_ladybug

package brain

// Write-путь L-9.4 (#233): git-коммит → Ladybug-граф (Commit/Person узлы +
// ребро AUTHORED Commit→Person). Идемпотентность — MERGE по id (repo:sha /
// email), повторный sync не плодит дублей. Чистая деривация плана живёт в
// commitplan.go (cgo-free), здесь только исполнение через execParams.

import lbug "github.com/LadybugDB/go-ladybug"

// UpsertCommit MERGE-ит один коммит и его граф.
func UpsertCommit(conn *lbug.Connection, in CommitInput) error {
	return UpsertCommits(conn, []CommitInput{in})
}

// UpsertCommits пишет пачку коммитов одной транзакцией. Person name-мердж:
// последний sync перезаписывает name при том же email (SET в MERGE) —
// дублей узлов нет; тот же email из mail-графа (D-1 #257) = тот же Person.
func UpsertCommits(conn *lbug.Connection, inputs []CommitInput) error {
	if len(inputs) == 0 {
		return nil
	}
	started := false
	if res, err := conn.Query("BEGIN TRANSACTION"); err == nil {
		qClose(res)
		started = true
	} else {
		qClose(res)
	}
	for i := range inputs {
		if err := upsertCommit(conn, &inputs[i]); err != nil {
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

// upsertCommit пишет один коммит: Person-автор, Commit-узел, AUTHORED.
// Коммит без email автора остаётся узлом без AUTHORED (сопряжение по email
// невозможно — commitAuthor ok=false). Email на узле Commit и в ребре —
// нормализованный (lowercase, как Person.id): git и mail сопрягаются.
func upsertCommit(conn *lbug.Connection, in *CommitInput) error {
	if in.ID == "" {
		return nil // коммит без канона id узлом не становится
	}
	email := in.Email
	p, hasAuthor := commitAuthor(*in)
	if hasAuthor {
		email = p.Email
		if err := execParams(conn, personUpsertQuery(), map[string]any{
			"id": p.ID, "name": p.Name, "email": p.Email,
		}); err != nil {
			return err
		}
	}
	if err := execParams(conn, commitUpsertQuery(), map[string]any{
		"id": in.ID, "repo": trimField(in.Repo), "subject": in.Subject,
		"author": in.Author, "email": email, "date": in.Date,
	}); err != nil {
		return err
	}
	if !hasAuthor {
		return nil
	}
	if err := execParams(conn, authoredEdgeQuery(), map[string]any{
		"cid": in.ID, "pid": p.ID,
	}); err != nil {
		return err
	}
	return nil
}
