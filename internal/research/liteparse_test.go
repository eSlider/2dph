package research

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureJSON = `{
  "total_pages": 1,
  "pages": [
    {
      "page": 1,
      "width": 612,
      "height": 792,
      "text": "INVOICE-7C2F9E\nClient: Acme GmbH\n",
      "text_items": [
        {"text": "INVOICE-7C2F9E", "x": 72, "y": 56.88, "width": 129.8, "height": 18.7, "font_name": "Helvetica", "font_size": 16, "confidence": 1},
        {"text": "Client: Acme GmbH", "x": 72, "y": 91.6, "width": 97.2, "height": 12.9, "font_name": "Helvetica", "font_size": 11, "confidence": 1}
      ],
      "blocks": [
        {"kind": "heading", "text": "INVOICE-7C2F9E", "level": 1, "bbox": {"x": 72, "y": 56.88, "width": 130, "height": 19}},
        {"kind": "paragraph", "text": "Client: Acme GmbH"},
        {"kind": "table", "text": "Item Qty Price", "bbox": {"x": 72, "y": 120, "width": 300, "height": 40}}
      ]
    }
  ]
}`

func TestJSONToYAMLConvertsBlocks(t *testing.T) {
	y, err := JSONToYAML([]byte(fixtureJSON))
	if err != nil {
		t.Fatalf("JSONToYAML: %v", err)
	}
	s := string(y)
	for _, want := range []string{"total_pages:", "pages:", "text_items:", "font_name:", "blocks:", "kind: heading"} {
		if !strings.Contains(s, want) {
			t.Errorf("YAML missing %q", want)
		}
	}
}

func TestJSONToYAMLRejectsInvalid(t *testing.T) {
	if _, err := JSONToYAML([]byte("{not json")); err == nil {
		t.Fatal("JSONToYAML accepted invalid JSON")
	}
}

func TestBlockKindsAndBBoxes(t *testing.T) {
	var l LitJSON
	if err := json.Unmarshal([]byte(fixtureJSON), &l); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	kinds := l.BlockKinds()
	if kinds["heading"] != 1 || kinds["paragraph"] != 1 || kinds["table"] != 1 {
		t.Errorf("BlockKinds = %#v, want heading=1 paragraph=1 table=1", kinds)
	}
	if n := l.BBoxCount(); n != 2 {
		t.Errorf("BBoxCount = %d, want 2 text_items", n)
	}
	if rt := l.RawText(); !strings.Contains(rt, "INVOICE-7C2F9E") || !strings.Contains(rt, "Acme") {
		t.Errorf("RawText = %q, want page text", rt)
	}
}

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := FileHash(p)
	if err != nil {
		t.Fatal(err)
	}
	// sha256("hello")
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if h != want {
		t.Errorf("FileHash = %s, want %s", h, want)
	}
}

func TestStructPath(t *testing.T) {
	if got := StructPath("abc123"); got != filepath.Join("var/struct-data", "abc123.yml") {
		t.Errorf("StructPath = %q, want var/struct-data/abc123.yml", got)
	}
}

func TestExtToFormat(t *testing.T) {
	cases := map[string]string{
		".pdf": "pdf", ".docx": "docx", ".xlsx": "xlsx", ".pptx": "pptx",
		".odt": "odt", ".png": "image", ".csv": "csv", ".md": "text",
		".txt": "text", ".unknown": "unknown",
	}
	for ext, want := range cases {
		if got := extToFormat(ext); got != want {
			t.Errorf("extToFormat(%s) = %q, want %q", ext, got, want)
		}
	}
}