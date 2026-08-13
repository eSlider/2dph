package mdleaves

import (
	"strings"
	"testing"
)

func TestSplitLeafsOnH2(t *testing.T) {
	body := "# Title\n\n## One\n\nalpha\n\n## Two\n\nbeta\n"
	leafs := SplitLeafs(map[string]string{}, body)
	if len(leafs) != 2 {
		t.Fatalf("n=%d", len(leafs))
	}
	if leafs[0].Heading != "One" || !strings.Contains(leafs[0].Text, "Title") {
		t.Fatalf("first=%+v", leafs[0])
	}
	if leafs[1].Heading != "Two" || strings.Contains(leafs[1].Text, "Title") {
		t.Fatalf("second=%+v", leafs[1])
	}
}

func TestSplitLeafsNoH2IsWholeDoc(t *testing.T) {
	body := "# Title\n\njust a paragraph\n"
	leafs := SplitLeafs(nil, body)
	if len(leafs) != 1 {
		t.Fatalf("n=%d", len(leafs))
	}
	if leafs[0].Heading != "Title" {
		t.Fatalf("heading=%q", leafs[0].Heading)
	}
	if !strings.Contains(leafs[0].Text, "just a paragraph") {
		t.Fatalf("text=%q", leafs[0].Text)
	}
}

func TestFrontmatterAndToAll(t *testing.T) {
	raw := "---\ntype: howto\nstatus: current\nrelated: docs/design.md\n---\n# Doc\n\n## Step\n\ndo it\n"
	got := ToAll(raw, "docs/x.md", "eSlider/2dph")
	if len(got) != 1 {
		t.Fatalf("n=%d", len(got))
	}
	if got[0].Type != "howto" || got[0].Status != "current" {
		t.Fatalf("%+v", got[0])
	}
	if got[0].Source != "docs/x.md" || got[0].Repo != "eSlider/2dph" {
		t.Fatalf("%+v", got[0])
	}
	if got[0].Related != "docs/design.md" {
		t.Fatalf("related=%q", got[0].Related)
	}
}

func TestEncodeJSONAndYAML(t *testing.T) {
	leafs := ToAll("# Hi\n\nbody\n", "a.md", "")
	js, err := EncodeJSON(leafs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, `"heading": "Hi"`) {
		t.Fatalf("json=%s", js)
	}
	y := EncodeYAML(leafs)
	if !strings.Contains(y, "heading: Hi") {
		t.Fatalf("yaml=%s", y)
	}
}

func TestDefaultsTypeAndStatus(t *testing.T) {
	got := ToAll("# Hi\n\nbody\n", "a.md", "")
	if got[0].Type != "reference" || got[0].Status != "current" {
		t.Fatalf("%+v", got[0])
	}
}
