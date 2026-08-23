package markdown

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/eSlider/2dph/internal/etl"
)

type Leaf struct {
	Source  string `json:"source"`
	Repo    string `json:"repo"`
	Heading string `json:"heading"`
	Text    string `json:"text"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Related string `json:"related"`
}

type chunk struct {
	Heading string
	Text    string
}

var (
	h1 = regexp.MustCompile(`^# \S`)
	h2 = regexp.MustCompile(`^## \S`)
)

func ExtractFrontmatter(text string) (map[string]string, string) {
	if !strings.HasPrefix(text, "---") {
		return map[string]string{}, text
	}
	end := strings.Index(text[3:], "\n---")
	if end == -1 {
		return map[string]string{}, text
	}
	fm := strings.TrimSpace(text[3 : 3+end])
	body := text[3+end+4:]
	meta := map[string]string{}
	for _, line := range strings.Split(fm, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return meta, body
}

func SplitLeafs(meta map[string]string, body string) []chunk {
	lines := strings.Split(body, "\n")
	title := ""
	type hdr struct {
		heading string
		start   int
	}
	var headers []hdr
	for i, line := range lines {
		switch {
		case h1.MatchString(line):
			title = strings.TrimSpace(strings.TrimLeft(line, "#"))
		case h2.MatchString(line):
			headers = append(headers, hdr{strings.TrimSpace(strings.TrimLeft(line, "#")), i})
		}
	}
	if len(headers) == 0 {
		var kept []string
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				kept = append(kept, l)
			}
		}
		return []chunk{{Heading: title, Text: strings.TrimSpace(strings.Join(kept, "\n"))}}
	}
	out := make([]chunk, 0, len(headers))
	for idx, h := range headers {
		end := len(lines)
		if idx+1 < len(headers) {
			end = headers[idx+1].start
		}
		var kept []string
		for _, l := range lines[h.start:end] {
			if strings.TrimSpace(l) != "" {
				kept = append(kept, l)
			}
		}
		text := strings.Join(kept, "\n")
		if idx == 0 && title != "" {
			text = title + "\n\n" + text
		}
		out = append(out, chunk{Heading: h.heading, Text: strings.TrimSpace(text)})
	}
	return out
}

func ToAll(text, path, repo string) []Leaf {
	meta, body := ExtractFrontmatter(text)
	if meta["type"] == "" {
		meta["type"] = "reference"
	}
	if meta["status"] == "" {
		meta["status"] = "current"
	}
	chunks := SplitLeafs(meta, body)
	out := make([]Leaf, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, Leaf{
			Source:  path,
			Repo:    repo,
			Heading: c.Heading,
			Text:    c.Text,
			Type:    meta["type"],
			Status:  meta["status"],
			Related: meta["related"],
		})
	}
	return out
}

func WalkMarkdown(root string) ([]string, error) {
	files, err := etl.WalkFiles(root, etl.WalkOptions{Exts: []string{".md", ".markdown"}})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out, nil
}

func EncodeJSON(leafs []Leaf) (string, error) {
	raw, err := json.MarshalIndent(leafs, "", "  ")
	if err != nil {
		return "", err
	}
	return string(raw) + "\n", nil
}
