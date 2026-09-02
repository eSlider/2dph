package incubator

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Message is one corpus message discovered by Scan: the root-relative .eml
// path, the target Dovecot mailbox (issue #252 п.3 folder map) and the
// canonical dedup key (Message-ID canon or body hash fallback).
type Message struct {
	Rel     string
	Mailbox string
	Key     string
	HasID   bool
}

// Scan walks a Thunderbird profile dump tree (epic #250 layout) and returns
// every importable message, path-ascending (root-flat messages first). Rules:
//   - each *.eml file is one message, except copies under an attachments/
//     directory (inline forwarded .eml — the гдеgroup corpus has exactly one,
//     and it is the single file without a Message-ID; attachments are never
//     corpus messages);
//   - the target mailbox comes from MailboxOfDir; numeric segments are the
//     per-message directory layer and are dropped.
func Scan(root string) ([]Message, error) {
	var out []Message
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "attachments" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".eml") {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("incubator: read %s: %w", p, err)
		}
		key, hasID, err := Key(raw)
		if err != nil {
			return fmt.Errorf("incubator: parse %s: %w", p, err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		out = append(out, Message{
			Rel:     filepath.ToSlash(rel),
			Mailbox: MailboxOfDir(dir),
			Key:     key,
			HasID:   hasID,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// MailboxOfDir maps a root-relative message directory onto the Dovecot target
// mailbox (issue #252 п.3):
//   - the profile root and numeric message-dir segments = INBOX;
//   - INBOX_sbd = the INBOX folder container (flat messages in it → INBOX);
//   - <Name>_sbd segments = folder <Name> (suffix stripped, MUTF-7 name
//     decoded — the corpus export encoded non-ASCII folder names, e.g.
//     "Mobilit&AOQ-t…" for "Mobilität…").
//
// Dovecot namespace separator is "/" (mail-server config), so the mailbox is
// INBOX or "INBOX/<Folder>/<Subfolder>/…".
func MailboxOfDir(dir string) string {
	segs := strings.Split(dir, "/")
	parts := make([]string, 0, len(segs))
	for _, s := range segs {
		if s == "" || isNumeric(s) {
			continue
		}
		if s == "INBOX_sbd" {
			parts = append(parts, "INBOX")
			continue
		}
		s = strings.TrimSuffix(s, "_sbd")
		if s == "" {
			continue
		}
		parts = append(parts, DecodeUTF7(s))
	}
	if len(parts) == 0 {
		return "INBOX"
	}
	return strings.Join(parts, "/")
}

// isNumeric reports whether a path segment is the per-message id directory
// layer (zero-padded Thunderbird message folders, e.g. "0000001").
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
