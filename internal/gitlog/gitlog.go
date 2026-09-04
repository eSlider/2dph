// Package gitlog reads commit history with go-git (no git binary).
package gitlog

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Options struct {
	Limit int
	Since time.Time
}

type Commit struct {
	SHA     string   `json:"sha"`
	Author  string   `json:"author"`
	Email   string   `json:"email"`
	Date    string   `json:"date"`
	Subject string   `json:"subject"`
	Files   []string `json:"files"`
}

type Leaf struct {
	Source  string `json:"source"`
	Repo    string `json:"repo"`
	Heading string `json:"heading"`
	Text    string `json:"text"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Related string `json:"related"`
}

// Log walks commits from HEAD, newest first, skipping merges.
func Log(repo string, opt Options) ([]Commit, error) {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return nil, err
	}
	logOpt := &git.LogOptions{Order: git.LogOrderCommitterTime}
	if !opt.Since.IsZero() {
		t := opt.Since
		logOpt.Since = &t
	}
	iter, err := r.Log(logOpt)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var out []Commit
	err = iter.ForEach(func(c *object.Commit) error {
		if c.NumParents() > 1 {
			return nil
		}
		if opt.Limit > 0 && len(out) >= opt.Limit {
			return Stop
		}
		files, ferr := changedFiles(c)
		if ferr != nil {
			return ferr
		}
		out = append(out, Commit{
			SHA:     c.Hash.String(),
			Author:  c.Author.Name,
			Email:   c.Author.Email,
			Date:    c.Author.When.Format(time.RFC3339),
			Subject: firstLine(c.Message),
			Files:   files,
		})
		return nil
	})
	if errors.Is(err, Stop) {
		err = nil
	}
	return out, err
}

// Stop ends a log walk early (limit reached).
var Stop = fmt.Errorf("gitlog: stop")

func changedFiles(c *object.Commit) ([]string, error) {
	var names []string
	if c.NumParents() == 0 {
		t, err := c.Tree()
		if err != nil {
			return nil, err
		}
		err = t.Files().ForEach(func(f *object.File) error {
			names = append(names, f.Name)
			return nil
		})
		sort.Strings(names)
		return names, err
	}
	parent, err := c.Parent(0)
	if err != nil {
		return nil, err
	}
	from, err := parent.Tree()
	if err != nil {
		return nil, err
	}
	to, err := c.Tree()
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTree(from, to)
	if err != nil {
		return nil, err
	}
	for _, ch := range changes {
		name := ch.To.Name
		if name == "" {
			name = ch.From.Name
		}
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func firstLine(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return strings.TrimSpace(msg[:i])
	}
	return strings.TrimSpace(msg)
}

func ToLeaf(c Commit, repo string) Leaf {
	short := c.SHA
	if len(short) > 12 {
		short = short[:12]
	}
	head := fmt.Sprintf("commit %s — %s", short, c.Subject)
	body := []string{
		fmt.Sprintf("commit %s in %s — %s", short, repo, c.Subject),
		fmt.Sprintf("Author: %s <%s>", c.Author, c.Email),
		fmt.Sprintf("Date: %s", c.Date),
	}
	if len(c.Files) > 0 {
		body = append(body, "Changing: "+strings.Join(c.Files, ", "))
	}
	return Leaf{
		Source:  repo + "@" + c.SHA,
		Repo:    repo,
		Heading: head,
		Text:    strings.Join(body, "\n"),
		Type:    "commit",
		Status:  "current",
		Related: strings.Join(c.Files, ","),
	}
}

func RepoName(repo string) (string, error) {
	r, err := git.PlainOpen(repo)
	if err != nil {
		return filepath.Base(repo), err
	}
	rem, err := r.Remote("origin")
	if err != nil {
		return filepath.Base(repo), nil
	}
	urls := rem.Config().URLs
	if len(urls) == 0 {
		return filepath.Base(repo), nil
	}
	u := strings.TrimSuffix(strings.TrimSuffix(urls[0], "/"), ".git")
	return path.Base(strings.ReplaceAll(u, "\\", "/")), nil
}
