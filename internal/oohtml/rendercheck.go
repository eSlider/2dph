package oohtml

import (
	"strconv"
	"strings"
)

// Code identifies a single render-check failure. Typed (no stringly-typed
// results) so callers can branch on the exact issue.
type Code string

const (
	IssueMissingLogo       Code = "missing-logo"
	IssueMissingParagraphs Code = "missing-paragraphs"
	IssueMissingGreeting   Code = "missing-greeting"
	IssueMissingOffer      Code = "missing-offer"
	IssueMissingSignature  Code = "missing-signature"
	IssueDuplicateChatline Code = "duplicate-chatline"
)

// CheckIssue is one render-check finding against a fetched mail HTML body.
type CheckIssue struct {
	Code   Code
	Detail string
}

func (i CheckIssue) String() string { return string(i.Code) + ": " + i.Detail }

// minParagraphs is the floor for a healthy branded mail: greeting, at least
// one body paragraph and the offer block each carry a <p>.
const minParagraphs = 2

// RenderCheck validates a fetched mail HTML body against the template that was
// requested. It returns zero issues when the render is healthy. This is the
// TDD gate: send must not happen while issues are non-empty.
func RenderCheck(html string, want TemplateData) []CheckIssue {
	var issues []CheckIssue

	add := func(c Code, d string) { issues = append(issues, CheckIssue{Code: c, Detail: d}) }

	if !strings.Contains(html, "cid:"+LogoCID) {
		add(IssueMissingLogo, "logo cid reference "+LogoCID+" not present (remote URL would be stripped by clients)")
	}
	if want.Greeting != "" && !strings.Contains(html, want.Greeting) {
		add(IssueMissingGreeting, "greeting "+want.Greeting+" not present")
	}
	if want.OfferHeading != "" && (!strings.Contains(html, want.OfferHeading) || !strings.Contains(html, want.CTALabel)) {
		add(IssueMissingOffer, "offer block (heading or CTA label) not present")
	}
	if want.SignerName != "" && (!strings.Contains(html, want.SignerName) || !strings.Contains(html, want.SiteURL)) {
		add(IssueMissingSignature, "signature (name or site link) not present")
	}

	paras := paragraphTexts(html)
	if len(paras) < minParagraphs {
		add(IssueMissingParagraphs, "expected at least "+strconv.Itoa(minParagraphs)+" paragraphs, got "+strconv.Itoa(len(paras)))
	}
	seen := map[string]int{}
	for _, p := range paras {
		seen[p]++
	}
	for text, n := range seen {
		if n > 1 {
			add(IssueDuplicateChatline, "text block duplicated "+strconv.Itoa(n)+"x: "+trunc(text, 48))
		}
	}
	return issues
}

// paragraphTexts extracts the trimmed text content of every <p>…</p> block.
func paragraphTexts(html string) []string {
	var out []string
	rest := html
	for {
		start := strings.Index(rest, "<p")
		if start < 0 {
			break
		}
		gt := strings.Index(rest[start:], ">")
		if gt < 0 {
			break
		}
		content := rest[start+gt+1:]
		end := strings.Index(content, "</p>")
		if end < 0 {
			break
		}
		text := strings.TrimSpace(stripTags(content[:end]))
		if text != "" {
			out = append(out, text)
		}
		rest = content[end:]
	}
	return out
}

// stripTags removes inline tags (e.g. <a>, <strong>) from a text block.
func stripTags(s string) string {
	var b strings.Builder
	for {
		lt := strings.Index(s, "<")
		if lt < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:lt])
		gt := strings.Index(s[lt:], ">")
		if gt < 0 {
			break
		}
		s = s[lt+gt+1:]
	}
	return b.String()
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
