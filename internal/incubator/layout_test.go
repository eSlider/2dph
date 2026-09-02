package incubator

import (
	"strings"
	"testing"
)

// owner is the wheregroup-period historical address (decision 2026-09-02,
// issue #252): the incubator mailbox IS this address, so messages are filed by
// whether the owner appears in From (Sent), To/CC/Delivered-To (INBOX) or
// nowhere (INBOX/Unmatched).
const owner = "andriy.oblivantsev@wheregroup.com"

// eml builds a raw message from header lines (no Message-Id needed — layout
// reads headers only, like Key()).
func eml(hdrs ...string) []byte {
	return []byte(strings.Join(hdrs, "\n") + "\n\nbody\n")
}

func TestLayoutOfRoutesByOwner(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{
			name: "from bare owner → Sent",
			raw:  eml("From: andriy.oblivantsev@wheregroup.com", "To: alle@wheregroup.com"),
			want: LayoutSent,
		},
		{
			name: "from display+address owner → Sent",
			raw:  eml("From: Andriy Oblivantsev <andriy.oblivantsev@wheregroup.com>", "To: alle@wheregroup.com"),
			want: LayoutSent,
		},
		{
			name: "from uppercase owner → Sent (case-insensitive)",
			raw:  eml("From: ANDRIY.OBLIVANTSEV@WHEREGROUP.COM"),
			want: LayoutSent,
		},
		{
			name: "from owner + foreign To → Sent wins over To",
			raw:  eml("From: andriy.oblivantsev@wheregroup.com", "To: someone@else.com"),
			want: LayoutSent,
		},
		{
			name: "To owner → INBOX",
			raw:  eml("From: boss@example.com", "To: andriy.oblivantsev@wheregroup.com"),
			want: LayoutInbox,
		},
		{
			name: "Cc owner → INBOX",
			raw:  eml("From: boss@example.com", "To: team@wheregroup.com", "Cc: andriy.oblivantsev@wheregroup.com"),
			want: LayoutInbox,
		},
		{
			name: "Delivered-To owner only → INBOX",
			raw:  eml("From: chiliproject@trac.wheregroup.com", "To: dev@wheregroup.com", "Delivered-To: andriy.oblivantsev@wheregroup.com"),
			want: LayoutInbox,
		},
		{
			name: "repeated Delivered-To lines + owner From → Sent (From precedence)",
			raw:  eml("From: Andriy Oblivantsev <andriy.oblivantsev@wheregroup.com>", "To: dev@wheregroup.com", "Delivered-To: entwicklung@mail-imap.wheregroup.lan", "Delivered-To: andriy.oblivantsev@wheregroup.com"),
			want: LayoutSent,
		},
		{
			name: "repeated Delivered-To only (foreign From) → INBOX",
			raw:  eml("From: list@wheregroup.com", "To: dev@wheregroup.com", "Delivered-To: entwicklung@mail-imap.wheregroup.lan", "Delivered-To: andriy.oblivantsev@wheregroup.com"),
			want: LayoutInbox,
		},
		{
			name: "owner nowhere → INBOX/Unmatched",
			raw:  eml("From: chiliproject@trac.wheregroup.com", "To: alle@wheregroup.com"),
			want: LayoutUnmatched,
		},
		{
			name: "owner only in display name of foreign address → not matched",
			raw:  eml("From: \"andriy.oblivantsev@wheregroup.com\" <boss@example.com>", "To: x@y.de"),
			want: LayoutUnmatched,
		},
		{
			name: "Bcc owner is not a routing signal → Unmatched",
			raw:  eml("From: boss@example.com", "To: team@wheregroup.com", "Bcc: andriy.oblivantsev@wheregroup.com"),
			want: LayoutUnmatched,
		},
		{
			name: "unparseable header with owner substring → substring fallback",
			raw:  eml("From: <@relay1.example.com:andriy.oblivantsev@wheregroup.com>", "To: x@y.de"),
			want: LayoutSent,
		},
		{
			name: "empty headers → Unmatched",
			raw:  eml("Subject: x"),
			want: LayoutUnmatched,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LayoutOf(tc.raw, owner)
			if err != nil {
				t.Fatalf("LayoutOf: %v", err)
			}
			if got != tc.want {
				t.Fatalf("LayoutOf = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLayoutOfRequiresOwner(t *testing.T) {
	if _, err := LayoutOf(eml("From: andriy.oblivantsev@wheregroup.com"), ""); err == nil {
		t.Fatal("LayoutOf with empty owner must fail")
	}
}
