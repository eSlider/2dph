package docbody

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/docgraph"
	"github.com/eslider/go-onlyoffice"
)

type fakeOO struct {
	folders   map[string]*onlyoffice.DavListing
	files     map[string][]byte
	downloads []string
}

func (f *fakeOO) ListDavFolder(ctx context.Context, id string) (*onlyoffice.DavListing, error) {
	l, ok := f.folders[id]
	if !ok {
		return nil, fmt.Errorf("fakeoo: no folder %s", id)
	}
	return l, nil
}

func (f *fakeOO) DownloadDavFile(ctx context.Context, id string, w io.Writer) (int64, error) {
	data, ok := f.files[id]
	if !ok {
		return 0, fmt.Errorf("fakeoo: no file %s", id)
	}
	f.downloads = append(f.downloads, id)
	n, err := w.Write(data)
	return int64(n), err
}

func davFile(id, title string) onlyoffice.DavFile {
	return onlyoffice.DavFile{ID: id, Title: title}
}

func TestOnlyOfficeFetcherByID(t *testing.T) {
	f := OnlyOfficeFetcher{Client: &fakeOO{files: map[string][]byte{"12345": []byte("%PDF body")}}}
	got, err := f.Fetch(context.Background(), docgraph.Row{OOPath: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "%PDF body" {
		t.Fatalf("bytes = %q", got)
	}
}

func TestOnlyOfficeFetcherFromRefFileID(t *testing.T) {
	oo := &fakeOO{files: map[string][]byte{"777": []byte("%PDF via ref")}}
	f := OnlyOfficeFetcher{Client: oo}
	got, err := f.Fetch(context.Background(), docgraph.Row{Ref: "https://vendor.test/doc?fileid=777&x=1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "%PDF via ref" {
		t.Fatalf("bytes = %q", got)
	}
	if len(oo.downloads) != 1 || oo.downloads[0] != "777" {
		t.Fatalf("downloads = %v", oo.downloads)
	}
}

func TestOnlyOfficeFetcherResolvesPath(t *testing.T) {
	oo := &fakeOO{
		folders: map[string]*onlyoffice.DavListing{
			"root": {Files: []onlyoffice.DavFile{}, Folders: []onlyoffice.DavFolder{{ID: "f1", Title: "o2"}}},
			"f1":   {Files: []onlyoffice.DavFile{davFile("42", "2026-09-01-o2-Rechnung-1.pdf")}},
		},
		files: map[string][]byte{"42": []byte("%PDF resolved")},
	}
	f := OnlyOfficeFetcher{Client: oo, RootID: "root", Prefix: "external"}
	got, err := f.Fetch(context.Background(), docgraph.Row{OOPath: "external/o2/2026-09-01-o2-Rechnung-1.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "%PDF resolved" {
		t.Fatalf("bytes = %q", got)
	}
}

func TestOnlyOfficeFetcherPathNotFound(t *testing.T) {
	oo := &fakeOO{folders: map[string]*onlyoffice.DavListing{"root": {}}}
	f := OnlyOfficeFetcher{Client: oo, RootID: "root"}
	if _, err := f.Fetch(context.Background(), docgraph.Row{OOPath: "external/o2/missing.pdf"}); err == nil {
		t.Fatal("want not-found error")
	}
}

func TestOnlyOfficeFetcherMaxBytes(t *testing.T) {
	oo := &fakeOO{files: map[string][]byte{"9": []byte(strings.Repeat("x", 128))}}
	f := OnlyOfficeFetcher{Client: oo, MaxBytes: 16}
	if _, err := f.Fetch(context.Background(), docgraph.Row{OOPath: "9"}); err == nil {
		t.Fatal("want size-limit error")
	}
}

func TestPathSegments(t *testing.T) {
	cases := []struct {
		path, prefix string
		want         []string
	}{
		{"external/o2/r.pdf", "external", []string{"o2", "r.pdf"}},
		{"External/O2/R.PDF", "external", []string{"O2", "R.PDF"}},
		{"/external/o2//r.pdf", "external", []string{"o2", "r.pdf"}},
		{"o2/r.pdf", "", []string{"o2", "r.pdf"}},
		{"o2/r.pdf", "external", []string{"o2", "r.pdf"}},
		{"", "external", nil},
	}
	for _, c := range cases {
		got := PathSegments(c.path, c.prefix)
		if strings.Join(got, "/") != strings.Join(c.want, "/") {
			t.Errorf("PathSegments(%q,%q) = %v, want %v", c.path, c.prefix, got, c.want)
		}
	}
}

func TestFileIDFromRef(t *testing.T) {
	cases := []struct{ in, want string }{
		{"12345", "12345"},
		{"external/o2/r.pdf", ""},
		{"https://x/y?fileid=777&a=1", "777"},
		{"https://x/y?file_id=42", "42"},
		{"https://x/y?fileid=", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := fileIDFromRef(c.in); got != c.want {
			t.Errorf("fileIDFromRef(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
