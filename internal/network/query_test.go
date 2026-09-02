//go:build cgo && system_ladybug

package network

// cgo-интеграция L-9.5 (#234): сеть читает mail+git граф из реальной
// Ladybug (temp DB, фикстура synthetic Alice/Bob/Carol/example.com) через
// LoadRows → BuildLinks. По образцу internal/brain/gitcommit_test.go.

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/canon"
)

func TestLoadRowsBuildLinksLiveDB(t *testing.T) {
	dir := t.TempDir()
	db, conn, err := brain.OpenWritable(filepath.Join(dir, "kb.lbug"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		t.Fatal(err)
	}

	// mail-фикстура: Alice→Bob (parent), Bob→Alice (reply), Alice→Carol (CC).
	parent := "m1@example.com"
	msgs := []brain.MessageInput{
		{
			Message: canon.Message{
				ID:       parent,
				ThreadID: "thread-1",
				Platform: "mail",
				From:     canon.Person{ID: "alice@example.com", Name: "Alice", Email: "alice@example.com"},
				To:       []canon.Person{{ID: "bob@example.com", Name: "Bob", Email: "bob@example.com"}},
				SentAt:   time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC),
			},
			Folder:   "INBOX",
			GatorRef: "kind=mail#v-aa11",
		},
		{
			Message: canon.Message{
				ID:       "m2@example.com",
				ThreadID: "thread-1",
				Platform: "mail",
				From:     canon.Person{ID: "bob@example.com", Name: "Bob", Email: "bob@example.com"},
				ReplyTo:  &parent,
				To:       []canon.Person{{ID: "alice@example.com", Name: "Alice", Email: "alice@example.com"}},
				SentAt:   time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC),
			},
			Folder:   "INBOX",
			GatorRef: "kind=mail#v-bb22",
		},
		{
			Message: canon.Message{
				ID:       "m3@example.com",
				ThreadID: "thread-2",
				Platform: "mail",
				From:     canon.Person{ID: "alice@example.com", Name: "Alice", Email: "alice@example.com"},
				To:       []canon.Person{{ID: "carol@example.com", Name: "Carol", Email: "carol@example.com"}},
				SentAt:   time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC),
			},
			Folder:   "INBOX",
			GatorRef: "kind=mail#v-cc33",
		},
	}
	if err := brain.UpsertMessages(conn, msgs); err != nil {
		t.Fatal(err)
	}

	// git-фикстура: Alice и Bob оба коммитили в demo-repo.
	commits := []brain.CommitInput{
		{
			ID: "demo-repo:1111111111111111111111111111111111111111", Repo: "demo-repo",
			SHA: "1111111111111111111111111111111111111111", Subject: "feat: mesh",
			Author: "Alice", Email: "alice@example.com", Date: "2026-08-10T12:00:00Z",
		},
		{
			ID: "demo-repo:2222222222222222222222222222222222222222", Repo: "demo-repo",
			SHA: "2222222222222222222222222222222222222222", Subject: "fix: typo",
			Author: "Bob", Email: "bob@example.com", Date: "2026-08-11T12:00:00Z",
		},
	}
	if err := brain.UpsertCommits(conn, commits); err != nil {
		t.Fatal(err)
	}

	rows, err := LoadRows(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Msgs) != 3 {
		t.Fatalf("Msgs = %d, want 3", len(rows.Msgs))
	}
	if len(rows.Commits) != 2 {
		t.Fatalf("Commits = %d, want 2", len(rows.Commits))
	}

	links := BuildLinks(rows, Filter{Person: "alice@example.com"})
	if len(links) < 2 {
		t.Fatalf("links = %+v, want bob+carol", links)
	}
	var bob, carol *Link
	for i := range links {
		switch links[i].Person {
		case "bob@example.com":
			bob = &links[i]
		case "carol@example.com":
			carol = &links[i]
		}
	}
	if bob == nil || bob.Msgs != 2 || bob.Replies != 1 {
		t.Fatalf("bob link = %+v, want 2 msgs + 1 reply", bob)
	}
	if len(bob.Projects) != 1 || bob.Projects[0].Repo != "demo-repo" {
		t.Fatalf("bob projects = %+v, want demo-repo", bob.Projects)
	}
	if bob.Verdict != "accept" {
		t.Fatalf("bob verdict = %q, want accept", bob.Verdict)
	}
	if carol == nil || carol.Msgs != 1 || carol.Verdict != "weaken" {
		t.Fatalf("carol link = %+v, want 1 msg weaken", carol)
	}

	// имя Person из графа доходит до связи
	if bob.Name != "Bob" {
		t.Fatalf("bob name = %q, want Bob", bob.Name)
	}
}
