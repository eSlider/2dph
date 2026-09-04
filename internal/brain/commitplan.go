package brain

// План записи git-коммита в Ladybug-граф (L-9.4, #233) — чистая логика без
// liblbug: деривация Person-автора из коммита и идемпотентные MERGE-запросы.
// Исполнение на живой БД — gitcommit.go (cgo).
//
// Схема (InitSchema, аддитивно к Leaf/Person/Message):
//
//	(:Commit)-[:AUTHORED]->(:Person)   автор коммита (id=email lowercase)
//
// Commit-узел несёт repo/subject/author/email/date; id = канон `repo:sha`
// (repo — имя из origin-remote, gitlog.RepoName) — стабильный при переезде
// клона, разводит одинаковые sha в разных репозиториях.

import (
	"strings"

	"github.com/eSlider/2dph/internal/canon"
)

// CommitInput — один git-коммит для записи. Коннектор (internal/gitgraph)
// мапит gitlog.Commit в эти поля: Email уже lowercase, ID = repo:sha.
type CommitInput struct {
	ID      string // канон узла: repo:sha
	Repo    string // имя репозитория (как leaf.Repo git-корпуса)
	SHA     string // полный commit sha (40 hex)
	Subject string // первая строка message
	Author  string // имя автора из git author
	Email   string // email автора (lowercase)
	Date    string // авторская дата RFC3339
}

// commitUpsertQuery — MERGE Commit по id (repo:sha); повторный sync
// перезаписывает свойства, дублей узла не создаёт.
func commitUpsertQuery() string {
	return `MERGE (c:Commit {id:$id})
SET c.repo=$repo, c.subject=$subject, c.author=$author,
    c.email=$email, c.date=$date`
}

// authoredEdgeQuery — MATCH Commit и Person + MERGE ребра AUTHORED
// (Commit→Person): ребро создаётся только если его ещё нет.
func authoredEdgeQuery() string {
	return `MATCH (c:Commit {id:$cid}), (p:Person {id:$pid}) MERGE (c)-[:AUTHORED]->(p)`
}

// commitAuthor деривирует Person-автора коммита: id = email lowercase
// (тот же канон, что Person mail-графа D-1 #257 — git и mail сопрягаются в
// один узел), name = display name из git author. Email пустой → ok=false
// (AUTHORED без email невозможен, Commit-узел всё равно пишется).
func commitAuthor(in CommitInput) (canon.Person, bool) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" {
		return canon.Person{}, false
	}
	return canon.Person{ID: email, Name: in.Author, Email: email}, true
}
