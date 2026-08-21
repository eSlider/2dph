package chat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestParseLinkedInInbox verifies get_inbox parsing against the v4.22 wire
// format (testdata/linkedin_inbox.json — synthetic Alice/Bob/Charlie).
func TestParseLinkedInInbox(t *testing.T) {
	text := readFixture(t, "linkedin_inbox.json")
	items := parseLinkedInInbox(text)
	if len(items) != 2 {
		t.Fatalf("expected 2 conversations (empty thread url skipped), got %d", len(items))
	}

	first := items[0]
	if first.ThreadID == "" {
		t.Error("expected thread id extracted from reference url")
	}
	if first.Participants != "Alice Example" {
		t.Errorf("participants=%q, want Alice Example", first.Participants)
	}
	if !strings.HasPrefix(first.ThreadID, "2-") {
		t.Errorf("unexpected thread id format %q", first.ThreadID)
	}
	if items[1].Participants != "Charlie Example" {
		t.Errorf("second participant=%q, want Charlie Example", items[1].Participants)
	}
}

// TestParseLinkedInInboxBadJSON verifies a non-JSON response yields no items
// rather than a panic or error.
func TestParseLinkedInInboxBadJSON(t *testing.T) {
	if got := parseLinkedInInbox("Session expired"); len(got) != 0 {
		t.Fatalf("expected no items for non-JSON, got %d", len(got))
	}
}

// TestParseLinkedInConversation verifies message extraction from the sections
// blob (testdata/linkedin_conversation.json — synthetic Alice/Bob).
func TestParseLinkedInConversation(t *testing.T) {
	text := readFixture(t, "linkedin_conversation.json")
	msgs := parseLinkedInConversation(text)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	if msgs[0].From != "Alice Example" {
		t.Errorf("from=%q, want Alice Example", msgs[0].From)
	}
	if !strings.Contains(msgs[0].Text, "Senior Software Engineer") {
		t.Errorf("alice text missing role, got %q", msgs[0].Text)
	}
	if msgs[0].Date == "" {
		t.Error("expected message date")
	}
	if msgs[1].From != "Bob Example" {
		t.Errorf("from=%q, want Bob Example", msgs[1].From)
	}
}

// TestParseLinkedInConversationEmpty verifies empty/non-JSON blobs parse to
// zero messages.
func TestParseLinkedInConversationEmpty(t *testing.T) {
	if got := parseLinkedInConversation("no data here"); len(got) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(got))
	}
}

// TestLinkedInTimestamp verifies weekday+clock resolution to a recent UTC date.
func TestLinkedInTimestamp(t *testing.T) {
	// The most recent Wednesday before/equal to "now".
	ts := linkedInTimestamp("WEDNESDAY", "10:02 AM")
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatalf("unparseable timestamp %q: %v", ts, err)
	}
	if parsed.Weekday() != time.Wednesday {
		t.Errorf("expected Wednesday, got %s", parsed.Weekday())
	}
	if parsed.Hour() != 10 || parsed.Minute() != 2 {
		t.Errorf("expected 10:02, got %02d:%02d", parsed.Hour(), parsed.Minute())
	}

	now := time.Now()
	diff := now.Sub(parsed)
	if diff < 0 || diff > 7*24*time.Hour {
		t.Errorf("timestamp %s is not within the last week of %s", parsed, now)
	}

	if got := linkedInTimestamp("MONDAY", "garbage"); got != "" {
		t.Errorf("expected empty for bad clock, got %q", got)
	}
	if got := linkedInTimestamp("", "1:22 PM"); got != "13:22" {
		t.Errorf("expected bare 13:22 for missing weekday, got %q", got)
	}

	// Relative day headers must resolve to full dates, not bare clocks.
	today := linkedInTimestamp("TODAY", "9:42 AM")
	yp, err := time.Parse(time.RFC3339, today)
	if err != nil {
		t.Fatalf("TODAY unparseable %q: %v", today, err)
	}
	if yp.Year() != now.Year() || yp.Month() != now.Month() || yp.Day() != now.Day() {
		t.Errorf("TODAY expected %v, got %v", now, yp)
	}
	yest := linkedInTimestamp("YESTERDAY", "3:00 PM")
	yp, err = time.Parse(time.RFC3339, yest)
	if err != nil {
		t.Fatalf("YESTERDAY unparseable %q: %v", yest, err)
	}
	if yp.Day() != now.AddDate(0, 0, -1).Day() {
		t.Errorf("YESTERDAY expected day %d, got %d", now.AddDate(0, 0, -1).Day(), yp.Day())
	}

	// MON DD header (e.g. "JUN 25"): must resolve to a full date. The
	// timestamp should fall within the current year (falling back to the
	// prior year if the date would be in the future).
	md := linkedInTimestamp("JUN 25", "10:48 AM")
	mp, err := time.Parse(time.RFC3339, md)
	if err != nil {
		t.Fatalf("MON DD unparseable %q: %v", md, err)
	}
	if mp.Year() != now.Year() && mp.Year() != now.Year()-1 {
		t.Errorf("JUN 25 expected year %d or %d, got %d", now.Year(), now.Year()-1, mp.Year())
	}
	if mp.Month() != time.June || mp.Day() != 25 {
		t.Errorf("JUN 25 expected Jun 25, got %s %d", mp.Month(), mp.Day())
	}
	if mp.After(now) {
		t.Errorf("JUN 25 resolved to the future: %s > %s", mp, now)
	}
}

// TestTransientLinkedInError verifies rate-limit errors are retryable but
// genuine failures are not.
func TestTransientLinkedInError(t *testing.T) {
	retryable := []string{
		"get_conversation error: Error calling tool 'get_conversation'",
		"get_conversation error: Unexpected error in get_conversation: net::ERR_HTTP_RESPONSE_CODE_FAILURE",
		"rpc error 503: rate limited",
		"rpc error 429: too many requests",
		"get_conversation: get_conversation error: This server still has a browser open on the profile.",
	}
	for _, msg := range retryable {
		if !isTransientLinkedInError(msg) {
			t.Errorf("expected %q to be transient", msg)
		}
	}
	permanent := []string{
		"get_inbox error: bad credentials",
		"rpc error -32602: Invalid request parameters",
		"unmarshal: unexpected end of JSON input",
	}
	for _, msg := range permanent {
		if isTransientLinkedInError(msg) {
			t.Errorf("expected %q to be permanent", msg)
		}
	}
}

// TestMsgLimitFor verifies the per-conversation message cap resolution.
func TestMsgLimitFor(t *testing.T) {
	if got := msgLimitFor(0); got != 100 {
		t.Errorf("expected default 100, got %d", got)
	}
	if got := msgLimitFor(5); got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

// TestWedged verifies the browser-open failure is recognized as a wedge.
func TestWedged(t *testing.T) {
	if !wedged(errors.New("get_conversation error: This server still has a browser open on the profile")) {
		t.Error("expected wedged error to be recognized")
	}
	if wedged(errors.New("get_conversation error: bad thing")) {
		t.Error("unexpected wedge detection")
	}
}

// TestThreadIDFromURL verifies thread id extraction.
func TestThreadIDFromURL(t *testing.T) {
	cases := []struct {
		url, want string
	}{
		{"/messaging/thread/2-abc123/", "2-abc123"},
		{"/messaging/thread/2-abc123", "2-abc123"},
		{"", ""},
		{"/messaging/thread/", ""},
	}
	for _, c := range cases {
		got := threadIDFromURL(c.url)
		if c.want == "" && validThreadID(got) {
			t.Errorf("threadIDFromURL(%q) = %q, want empty", c.url, got)
		}
		if c.want != "" && got != c.want {
			t.Errorf("threadIDFromURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}
