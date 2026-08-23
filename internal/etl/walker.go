// Safe file walker used by ETL import paths (#96). Hardens the walk against
// path traversal, symlink escapes outside the root, unbounded recursion
// (depth limit), binary/huge files, and returns files in deterministic order.
package etl

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultMaxDepth = 32

// WalkOptions tunes WalkFiles. Zero values select safe defaults.
type WalkOptions struct {
	// MaxDepth limits directory recursion relative to the walk root.
	// Non-positive means defaultMaxDepth.
	MaxDepth int
	// MaxBytes skips files larger than this many bytes. Non-positive = no limit.
	MaxBytes int64
	// SkipBinary skips files whose first bytes contain a NUL (binary heuristic).
	SkipBinary bool
	// Exts, when non-empty, keeps only files with one of these extensions
	// (lower-case, including the leading dot, e.g. ".md"). Matching is
	// case-insensitive.
	Exts []string
}

// File is one file discovered by WalkFiles.
type File struct {
	// Path is the absolute path under the walk root.
	Path string
	// Rel is the root-relative, slash-separated path (deterministic sort key).
	Rel string
	// Size is the file size in bytes.
	Size int64
}

// WalkFiles walks root and returns the files under it that pass the safety
// and filter rules, in deterministic (lexicographic Rel) order. Symlinks are
// never followed by default: a symlink pointing outside root is rejected and
// a symlinked directory is not descended into (prevents cycles and escapes).
func WalkFiles(root string, opts WalkOptions) ([]File, error) {
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	if _, err := os.Stat(root); err != nil {
		return nil, err
	}
	// Normalize filter extensions once.
	for i := range opts.Exts {
		opts.Exts[i] = strings.ToLower(opts.Exts[i])
	}

	var out []File
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsPermission(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return handleSymlink(root, path, d, opts, &out)
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return filepath.SkipDir // reject traversal / escape
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if depthOf(rel) >= opts.MaxDepth {
				return filepath.SkipDir // depth limit honored
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		return addFile(path, rel, info, opts, &out)
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, nil
}

// handleSymlink applies the symlink policy. By default symlinks are skipped
// (not followed), which inherently prevents escaping the root.
func handleSymlink(root, path string, d os.DirEntry, opts WalkOptions, out *[]File) error {
	info, ierr := os.Stat(path)
	if ierr != nil {
		return nil // broken symlink
	}
	if info.IsDir() {
		// Never descend into a symlinked directory (cycles / escapes).
		return filepath.SkipDir
	}
	// Symlinked file: only keep it when it resolves inside the root.
	target, terr := filepath.EvalSymlinks(path)
	if terr != nil {
		return nil
	}
	if !within(root, target) {
		return nil // symlink-to-outside rejected
	}
	return addFile(path, "", info, opts, out)
}

// addFile applies size / binary / extension filters and appends to out.
// rel may be empty for symlinked files, in which case the path itself is used
// as the sort key.
func addFile(path, rel string, info os.FileInfo, opts WalkOptions, out *[]File) error {
	if rel == "" {
		rel = filepath.ToSlash(path)
	}
	if len(opts.Exts) > 0 && !hasExt(rel, opts.Exts) {
		return nil
	}
	if opts.MaxBytes > 0 && info.Size() > opts.MaxBytes {
		return nil // huge-file skip
	}
	if opts.SkipBinary && isBinary(path) {
		return nil
	}
	*out = append(*out, File{Path: path, Rel: rel, Size: info.Size()})
	return nil
}

// within reports whether path stays inside root (rejects ".." escapes).
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// hasExt reports whether the lower-cased extension of rel matches any filter.
func hasExt(rel string, exts []string) bool {
	e := strings.ToLower(filepath.Ext(rel))
	for _, want := range exts {
		if e == want {
			return true
		}
	}
	return false
}

// depthOf returns the number of path segments in a root-relative path.
func depthOf(rel string) int {
	return strings.Count(rel, string(os.PathSeparator)) + 1
}

// isBinary reports whether path looks binary by scanning its first bytes for
// a NUL byte (classic heuristic).
func isBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}
