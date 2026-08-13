// Package websearch is the SearXNG client used as the second independent source.
//
// An empty result list from this instance is throttling, not evidence of absence.
package websearch

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	StatusOK        = "ok"
	StatusThrottled = "throttled"

	DefaultLimit        = 5
	DefaultSnippetChars = 150
	MinInterval         = 10.0
	CacheTTL            = 7 * 24 * 3600
)

var RetryBackoff = []float64{20, 60}

type Payload struct {
	Query               string     `json:"query"`
	Results             []RawHit   `json:"results"`
	UnresponsiveEngines [][]string `json:"unresponsive_engines"`
}

type RawHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Engine  string `json:"engine"`
}

type Hit struct {
	Rank    int    `json:"rank"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Engine  string `json:"engine"`
}

type Output struct {
	Query        string   `json:"query"`
	Status       string   `json:"status"`
	Results      []Hit    `json:"results"`
	Unresponsive []string `json:"unresponsive,omitempty"`
	Note         string   `json:"note,omitempty"`
	Cached       bool     `json:"cached,omitempty"`
}

func Classify(p Payload) string {
	if len(p.Results) > 0 {
		return StatusOK
	}
	return StatusThrottled
}

func Project(p Payload, limit, snippetChars int) Output {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if snippetChars <= 0 {
		snippetChars = DefaultSnippetChars
	}
	status := Classify(p)
	n := limit
	if n > len(p.Results) {
		n = len(p.Results)
	}
	hits := make([]Hit, 0, n)
	for i := 0; i < n; i++ {
		item := p.Results[i]
		hits = append(hits, Hit{
			Rank:    i + 1,
			Title:   item.Title,
			URL:     item.URL,
			Snippet: trimSnippet(item.Content, snippetChars),
			Engine:  item.Engine,
		})
	}
	out := Output{
		Query:   p.Query,
		Status:  status,
		Results: hits,
	}
	for _, pair := range p.UnresponsiveEngines {
		if len(pair) >= 2 {
			out.Unresponsive = append(out.Unresponsive, pair[0]+": "+pair[1])
		} else if len(pair) == 1 {
			out.Unresponsive = append(out.Unresponsive, pair[0])
		}
	}
	if status == StatusThrottled {
		out.Note = "no engine answered - this is a throttled instance, not evidence that nothing exists"
	}
	return out
}

var spaceRE = regexp.MustCompile(`\s+`)

func trimSnippet(s string, max int) string {
	s = strings.TrimSpace(spaceRE.ReplaceAllString(s, " "))
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	cut := strings.TrimRightFunc(string(runes[:max]), unicode.IsSpace)
	return cut + "..."
}

func CacheKey(query string, params map[string]string) string {
	norm := strings.Join(strings.Fields(strings.ToLower(query)), " ")
	if params == nil {
		params = map[string]string{}
	}
	stable, _ := json.Marshal(params)
	sum := sha256.Sum256([]byte(norm + "\x00" + string(stable)))
	return hex.EncodeToString(sum[:])
}

func WaitFor(last *float64, now, interval float64) float64 {
	if last == nil {
		return 0
	}
	d := interval - (now - *last)
	if d < 0 {
		return 0
	}
	return d
}

func PHIReason(query string) string {
	for _, p := range phiPatterns {
		if p.re.MatchString(query) {
			return p.reason
		}
	}
	return ""
}

type phiPat struct {
	re     *regexp.Regexp
	reason string
}

var phiPatterns = []phiPat{
	{regexp.MustCompile(`\d{6,}`), "a run of six or more digits looks like an ID"},
	{regexp.MustCompile(`(?i)\bpersonalnummer\b`), "Personalnummer is staff data"},
	{regexp.MustCompile(`(?i)\bkv[-\s]?nr\b`), "KV-Nr is an insurance number"},
	{regexp.MustCompile(`(?i)\bversichertennummer\b`), "insurance number"},
	{regexp.MustCompile(`(?i)\b[A-Za-zÄÖÜäöüß]+(?:stra(?:ss|ß)e|str\.)\s*\d+`), "a street with a house number looks like an address"},
	{regexp.MustCompile(`(?i)\bgeb(?:urtsdatum)?\.?\s*\d{1,2}[./]\d{1,2}[./]\d{2,4}`), "a date of birth"},
}

func (o Output) YAML() string {
	var b strings.Builder
	fmt.Fprintf(&b, "query: %s\n", yamlScalar(o.Query))
	fmt.Fprintf(&b, "status: %s\n", yamlScalar(o.Status))
	if len(o.Results) == 0 {
		b.WriteString("results: []\n")
	} else {
		b.WriteString("results:\n")
		for _, r := range o.Results {
			b.WriteString("-\n")
			fmt.Fprintf(&b, "  rank: %d\n", r.Rank)
			fmt.Fprintf(&b, "  title: %s\n", yamlScalar(r.Title))
			fmt.Fprintf(&b, "  url: %s\n", yamlScalar(r.URL))
			fmt.Fprintf(&b, "  snippet: %s\n", yamlScalar(r.Snippet))
			fmt.Fprintf(&b, "  engine: %s\n", yamlScalar(r.Engine))
		}
	}
	if len(o.Unresponsive) > 0 {
		b.WriteString("unresponsive:\n")
		for _, u := range o.Unresponsive {
			fmt.Fprintf(&b, "- %s\n", yamlScalar(u))
		}
	}
	if o.Note != "" {
		fmt.Fprintf(&b, "note: %s\n", yamlScalar(o.Note))
	}
	if o.Cached {
		b.WriteString("cached: true\n")
	}
	return b.String()
}

func yamlScalar(s string) string {
	if strings.Contains(s, "\n") {
		b, _ := json.Marshal(s)
		return string(b)
	}
	if s == "" || strings.ContainsAny(s, ":#'\"[]{}&*!|>%@`") || s != strings.TrimSpace(s) {
		b, _ := json.Marshal(s)
		return string(b)
	}
	return s
}
