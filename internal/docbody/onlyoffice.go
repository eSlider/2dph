package docbody

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/eSlider/2dph/internal/docgraph"
	"github.com/eslider/go-onlyoffice"
)

// OOClient is the subset of the OnlyOffice Files API used by
// OnlyOfficeFetcher. *onlyoffice.Client satisfies it; tests substitute a fake.
type OOClient interface {
	ListDavFolder(ctx context.Context, id string) (*onlyoffice.DavListing, error)
	DownloadDavFile(ctx context.Context, id string, w io.Writer) (int64, error)
}

// DefaultMaxBytes caps a downloaded artifact (safety net for the temp copy).
const DefaultMaxBytes = 32 << 20

// *onlyoffice.Client is the production OOClient.
var _ OOClient = (*onlyoffice.Client)(nil)

// OnlyOfficeFetcher downloads a document's bytes from OnlyOffice. The file is
// located by, in order of precedence:
//  1. a numeric file id in oo_path or ref;
//  2. a fileid=<n> query in ref;
//  3. oo_path as a path below RootID (Prefix segment stripped), resolved by
//     walking the Files tree.
//
// No host URL or folder id is hardcoded: the client and RootID come from the
// caller (env in bin/mail/leaf.go).
type OnlyOfficeFetcher struct {
	Client   OOClient
	RootID   string // folder id that roots oo_path (e.g. the external folder)
	Prefix   string // leading oo_path segment to strip (default "external")
	MaxBytes int64  // <=0 → DefaultMaxBytes
}

// Fetch resolves and downloads the artifact for r.
func (f OnlyOfficeFetcher) Fetch(ctx context.Context, r docgraph.Row) ([]byte, error) {
	id := fileIDFromRef(r.OOPath)
	if id == "" {
		id = fileIDFromRef(r.Ref)
	}
	if id == "" {
		var err error
		if id, err = f.resolvePath(ctx, r.OOPath); err != nil {
			return nil, err
		}
	}
	var buf bytes.Buffer
	max := f.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	if _, err := f.Client.DownloadDavFile(ctx, id, &limitWriter{w: &buf, max: max}); err != nil {
		return nil, fmt.Errorf("docbody: download %s: %w", id, err)
	}
	return buf.Bytes(), nil
}

// limitWriter fails the write once the artifact would exceed max bytes. It
// accepts the bytes within the cap, then reports the overflow as an error so
// the download stops (io.Copy propagates it).
type limitWriter struct {
	w   io.Writer
	n   int64
	max int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	room := l.max - l.n
	if room <= 0 {
		return 0, fmt.Errorf("docbody: artifact exceeds %d bytes", l.max)
	}
	if int64(len(p)) > room {
		n, err := l.w.Write(p[:room])
		l.n += int64(n)
		if err == nil {
			err = fmt.Errorf("docbody: artifact exceeds %d bytes", l.max)
		}
		return n, err
	}
	n, err := l.w.Write(p)
	l.n += int64(n)
	return n, err
}

// resolvePath walks the Files tree from RootID along oo_path and returns the
// id of the final file.
func (f OnlyOfficeFetcher) resolvePath(ctx context.Context, ooPath string) (string, error) {
	segs := PathSegments(ooPath, f.Prefix)
	if len(segs) == 0 {
		return "", fmt.Errorf("docbody: empty oo_path")
	}
	if f.RootID == "" {
		return "", fmt.Errorf("docbody: root folder id is required to resolve %q", ooPath)
	}
	dir := f.RootID
	for i, seg := range segs {
		listing, err := f.Client.ListDavFolder(ctx, dir)
		if err != nil {
			return "", fmt.Errorf("docbody: list %s: %w", dir, err)
		}
		if i == len(segs)-1 {
			for _, file := range listing.Files {
				if strings.EqualFold(file.Title, seg) {
					return file.ID, nil
				}
			}
			return "", fmt.Errorf("docbody: file %q not found in folder %s", seg, dir)
		}
		next := ""
		for _, folder := range listing.Folders {
			if strings.EqualFold(folder.Title, seg) {
				next = folder.ID
				break
			}
		}
		if next == "" {
			return "", fmt.Errorf("docbody: folder %q not found under %s", seg, dir)
		}
		dir = next
	}
	return "", fmt.Errorf("docbody: %q not resolved", ooPath)
}

// PathSegments splits an oo_path into path segments, stripping a leading
// Prefix segment (case-insensitive) when present. Empty segments are dropped,
// so "external/o2/r.pdf" with prefix "external" → ["o2", "r.pdf"].
func PathSegments(ooPath, prefix string) []string {
	parts := strings.Split(strings.ReplaceAll(ooPath, "\\", "/"), "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p == "" || p == "." {
			continue
		}
		out = append(out, p)
	}
	if prefix = strings.TrimSpace(prefix); prefix != "" && len(out) > 0 &&
		strings.EqualFold(out[0], prefix) {
		out = out[1:]
	}
	return out
}

// fileIDFromRef extracts a numeric OnlyOffice file id from a bare id or a
// "fileid=<n>" (or "file_id=<n>") query fragment. Returns "" when there is no
// numeric id.
func fileIDFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if isDigits(ref) {
		return ref
	}
	lower := strings.ToLower(ref)
	for _, key := range []string{"fileid=", "file_id="} {
		idx := strings.Index(lower, key)
		if idx < 0 {
			continue
		}
		rest := ref[idx+len(key):]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end > 0 {
			return rest[:end]
		}
	}
	return ""
}

// isDigits reports whether s is a non-empty run of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
