package sync

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRetrySucceedsOnSecondTry(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}, func() error {
		attempts++
		if attempts == 1 {
			return retryWrap(errors.New("boom"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetryExhaustsAttempts(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}, func() error {
		attempts++
		return retryWrap(errors.New("nope"))
	})
	if err == nil {
		t.Fatal("expected error after exhaustion")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryNonRetriableAbortsImmediately(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond}, func() error {
		attempts++
		return errors.New("permanent")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt for non-retriable, got %d", attempts)
	}
}

func TestRetryRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Retry(ctx, RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond}, func() error {
		return retryWrap(errors.New("x"))
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDelayGrows(t *testing.T) {
	p := RetryPolicy{BaseDelay: time.Second, MaxDelay: 30 * time.Second, Jitter: 0}
	d1 := p.delay(1) // attempt 1 => 0
	d2 := p.delay(2)
	d3 := p.delay(3)
	if d1 != 0 {
		t.Fatalf("attempt 1 delay should be 0, got %v", d1)
	}
	if d2 != time.Second {
		t.Fatalf("attempt 2 delay should be 1s, got %v", d2)
	}
	if d3 != 2*time.Second {
		t.Fatalf("attempt 3 delay should be 2s, got %v", d3)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"a/b\\c:d*e": "a_b_c_d_e",
		"normal.txt": "normal.txt",
		"../evil":    ".._evil",
		"a b c.pdf":  "a_b_c.pdf",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsICS(t *testing.T) {
	if !isICS("reply.ics") || !isICS("x.ICAL") {
		t.Fatal("ics extensions not detected")
	}
	if isICS("invoice.pdf") {
		t.Fatal("pdf misdetected as ics")
	}
}

const fixtureReplyICS = `BEGIN:VCALENDAR
METHOD:REPLY
PRODID:Microsoft Exchange Server 2010
VERSION:2.0
BEGIN:VTIMEZONE
TZID:W. Europe Standard Time
BEGIN:STANDARD
DTSTART:16010101T030000
TZOFFSETFROM:+0200
TZOFFSETTO:+0100
RRULE:FREQ=YEARLY;INTERVAL=1;BYDAY=-1SU;BYMONTH=10
END:STANDARD
BEGIN:DAYLIGHT
DTSTART:16010101T020000
TZOFFSETFROM:+0100
TZOFFSETTO:+0200
RRULE:FREQ=YEARLY;INTERVAL=1;BYDAY=-1SU;BYMONTH=3
END:DAYLIGHT
END:VTIMEZONE
BEGIN:VEVENT
ATTENDEE;PARTSTAT=ACCEPTED;CN="Baker, Ben":mailto:bbaker1@teksystems.com
UID:bvlnr1i35ug30kn6rvu9dop00g@google.com
SUMMARY;LANGUAGE=en-US:Accepted: Appointment (Ben  Baker)
DTSTART;TZID=W. Europe Standard Time:20260812T120000
DTEND;TZID=W. Europe Standard Time:20260812T123000
CLASS:PUBLIC
STATUS:CONFIRMED
LOCATION;LANGUAGE=en-US:https://meet.google.com/sxh-ubud-jrd
END:VEVENT
END:VCALENDAR`

func TestICSToMarkdown(t *testing.T) {
	out := ICSToMarkdown([]byte(fixtureReplyICS))
	for _, want := range []string{
		"Accepted: Appointment",
		"When:",
		"Where:",
		"meet.google.com",
		"Attendee:",
		"Baker, Ben",
		"Accepted",
		"Calendar method: REPLY",
	} {
		if !contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if contains(out, "BEGIN:VCALENDAR") {
		t.Errorf("raw ICS leaked into markdown:\n%s", out)
	}
}

func TestICSToMarkdownFallback(t *testing.T) {
	out := ICSToMarkdown([]byte("not a calendar"))
	if !contains(out, "not a calendar") {
		t.Fatalf("expected raw fallback, got %q", out)
	}
}

func TestICSToMarkdownNormalizesLatin1(t *testing.T) {
	// Real-world ICS from a rental portal: summary in UTF-8, location Latin-1
	// ("N\xfcrnberg"). The markdown output must be valid UTF-8 everywhere.
	raw := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\n" +
		"SUMMARY:Mietwagen-Buchung: N\xc3\xbcrnberg\r\n" +
		"LOCATION:N\xfcrnberg\r\nDTSTART:20200101T090000Z\r\nDTEND:20200101T180000Z\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	out := ICSToMarkdown([]byte(raw))
	if !utf8.ValidString(out) {
		t.Fatalf("output is not valid UTF-8:\n%q", out)
	}
	if !contains(out, "Nürnberg") {
		t.Errorf("expected Nürnberg in output:\n%s", out)
	}
	if contains(out, "N\xfcrnberg") {
		t.Errorf("Latin-1 bytes leaked into output:\n%q", out)
	}
}

func TestICSToMarkdownAllDay(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:y@google.com
SUMMARY:All day thing
DTSTART;VALUE=DATE:20260815
DTEND;VALUE=DATE:20260816
END:VEVENT
END:VCALENDAR`
	out := ICSToMarkdown([]byte(ics))
	if !contains(out, "All day thing") || !contains(out, "2026-08-15") {
		t.Errorf("all-day event not parsed:\n%s", out)
	}
}

func TestCollectParts(t *testing.T) {
	p := gmailPart{
		MimeType: "multipart/mixed",
		Parts: []gmailPart{
			{MimeType: "multipart/alternative", Parts: []gmailPart{
				{MimeType: "text/plain", Body: gmailBody{Data: b64("plain text")}},
				{MimeType: "text/html", Body: gmailBody{Data: b64("<p>html</p>")}},
			}},
			{PartID: "2", MimeType: "application/pdf", Filename: "invoice.pdf", Body: gmailBody{Size: 100}},
		},
	}
	text, html, atts := collectParts(p, "root", "abc123", 0)
	if text != "plain text" {
		t.Errorf("text = %q", text)
	}
	if html != "<p>html</p>" {
		t.Errorf("html = %q", html)
	}
	if len(atts) != 1 || atts[0].FileName != "invoice.pdf" || atts[0].FileID != "abc123:2" {
		t.Errorf("atts = %+v", atts)
	}
}

func b64(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
