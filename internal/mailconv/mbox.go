// mbox -> mailconv.Message import for the disk mail gap (issue #79).
//
// The mbox splitter lives here (not in a build-tagged script) so it is
// importable and unit-testable offline: any caller - bin/mail/convert-mbox.go or
// a future Source adapter (#97) - splits an mbox stream into raw RFC822 messages
// and converts them through the shared parseEML path into mailconv.Message with
// a caller-supplied source-tag. Output is content-addressed, so re-runs never
// duplicate (single implementation, idempotent).
package mailconv

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// reSkipFolder matches mail folders the import policy excludes (#79): drafts,
// templates, trash, junk/spam and the unsent queue, plus the German Outlook
// folder names readpst emits (Entwürfe=Drafts, Vorlagen=Templates,
// Gelöschte Objekte=Deleted Items, Junk-E-Mail, Postausgang=Outbox/Unsent).
// The ".sbd" (Thunderbird subfolder marker) suffix is folded in.
var reSkipFolder = regexp.MustCompile(`(?i)^(drafts?|templates?|trash|junk|spam(assassin)?|unsent( messages)?|entwürfe|vorlagen|gelöschte objekte|junk-e-mail|postausgang)(\.sbd)?$`)

// SkipFolder reports whether the import policy excludes a mail folder (#79).
// It is the single implementation of the folder exclusion policy: the mbox
// splitter and the PST source adapter both consult it.
func SkipFolder(name string) bool { return reSkipFolder.MatchString(name) }

// reEnvelope matches the sender + ctime-date tail of a real mbox separator.
var reEnvelope = regexp.MustCompile(`^\S+ [A-Z][a-z]{2} [A-Z][a-z]{2} `)

// isSep reports whether a line is a real mbox separator. Gmail/Thunderbird
// mbox uses a bare "From " (empty tail) or "From sender ctime-date"; lines like
// "From FCC: ..." (a Thunderbird fcc marker) and "From: ..." headers must not
// split.
func isSep(line []byte) bool {
	s := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
	if !strings.HasPrefix(s, "From ") {
		return false
	}
	rest := s[len("From "):]
	return rest == "" || reEnvelope.MatchString(rest)
}

// SplitMbox splits an mbox stream into one raw RFC822 message per real "From "
// separator. A message's leading separator is a delimiter and is not part of
// the RFC 822 body, so it is dropped.
func SplitMbox(r io.Reader) ([][]byte, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var (
		out   [][]byte
		cur   bytes.Buffer
		start bool
	)
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		msg := make([]byte, cur.Len())
		copy(msg, cur.Bytes())
		out = append(out, msg)
		cur.Reset()
	}
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if isSep(line) {
				// A separator begins a new message; flush the previous one and
				// skip the separator itself.
				if start {
					flush()
				}
				start = true
				if err == io.EOF {
					break
				}
				continue
			}
			if start {
				cur.Write(line)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	flush()
	return out, nil
}

// MboxMessage converts one raw mbox message into a Message through the shared
// parseEML path, tagging it with the caller-supplied source and folder (e.g.
// "tb-backup/gridfactor" + "Inbox").
func MboxMessage(raw []byte, source, folder string) (Message, error) {
	res, err := parseEML(bytes.NewReader(raw))
	if err != nil {
		return Message{}, err
	}
	res.msg.Source = source
	res.msg.Folder = folder
	return res.msg, nil
}

// LooksMbox reports whether a file begins with the mbox "From " separator,
// scanning the first few lines so metadata files are cheaply skipped.
func LooksMbox(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, 8<<20))
	// ReadBytes keeps the trailing \r (ReadLine strips it, breaking bare
	// "From " separators).
	for i := 0; i < 6; i++ {
		line, err := r.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			break
		}
		if isSep(line) {
			return true
		}
	}
	return false
}

// SplitMboxDir walks root for mbox files (skipping Thunderbird metadata and
// policy folders) and writes each message to
// out/<source>/<rel-dir>/<sha256:16>/<sha256:16>.eml. The output is
// content-addressed so a re-run skips already-written messages and never
// duplicates (idempotency, like #74's seen-set). source is the label used in
// message.json; when empty it is derived from the top-level dir under root.
// dryRun counts without writing. Returns (written, skipped, err).
func SplitMboxDir(root, out, source string, dryRun bool) (written, skipped int, err error) {
	err = filepath.Walk(root, func(p string, info os.FileInfo, werr error) error {
		if werr != nil || info.IsDir() {
			return werr
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".msf", ".dat", ".js", ".json", ".sqlite", ".sqlite-wal":
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
			if SkipFolder(seg) {
				return nil
			}
		}
		if !LooksMbox(p) {
			return nil
		}
		n, s, serr := splitMboxToDir(p, out, source, rel, dryRun)
		if serr != nil {
			return serr
		}
		written += n
		skipped += s
		return nil
	})
	return written, skipped, err
}

// splitMboxToDir splits one mbox file and writes its messages under
// out/<source>/<rel-dir>/<sha256:16>/<sha256:16>.eml, skipping targets that
// already exist.
func splitMboxToDir(p, out, source, rel string, dryRun bool) (written, skipped int, err error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	msgs, err := SplitMbox(f)
	if err != nil {
		return 0, 0, err
	}
	if source == "" {
		if i := strings.IndexByte(rel, filepath.Separator); i > 0 {
			source = rel[:i]
		} else {
			source = rel
		}
	}
	base := []string{out, SanitizeSegment(source)}
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		if seg != "" && seg != "." {
			base = append(base, SanitizeSegment(seg))
		}
	}
	baseDir := filepath.Join(base...)
	for _, raw := range msgs {
		sum := sha256.Sum256(raw)
		id := hex.EncodeToString(sum[:8]) // 16 hex chars
		if dryRun {
			written++
			continue
		}
		dir := filepath.Join(baseDir, id)
		target := filepath.Join(dir, id+".eml")
		if _, err := os.Stat(target); err == nil {
			skipped++
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return written, skipped, err
		}
		if err := os.WriteFile(target, raw, 0o644); err != nil {
			return written, skipped, err
		}
		written++
	}
	return written, skipped, nil
}

// SanitizeSegment makes a path segment filesystem-safe for corpus output dirs
// (single implementation: mbox splitter and the PST source adapter both use
// it, so corpus layouts stay consistent).
func SanitizeSegment(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_",
		"<", "_", ">", "_", "|", "_", " ", "_", "[", "", "]", "", ".", "_")
	return r.Replace(s)
}
