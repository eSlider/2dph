package incubator

import (
	"strings"
	"testing"
)

func TestCanonMessageID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"<Foo.Bar@EXAMPLE.com>", "foo.bar@example.com"}, // brackets trimmed, case folded
		{"foo@example.com", "foo@example.com"},           // no brackets
		{"  <X@y.example>  ", "x@y.example"},             // outer space trimmed
		{"", ""},                                         // empty stays empty
		{"<a@b.example> <c@d.example>", "a@b.example"},   // first of several ids
		{"<chiliproject.issue-4997.20151022082248@trac.wheregroup.com>",
			"chiliproject.issue-4997.20151022082248@trac.wheregroup.com"},
	}
	for _, tc := range tests {
		if got := CanonMessageID(tc.in); got != tc.want {
			t.Errorf("CanonMessageID(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

const emlWithID = `Return-Path: <alice@example.com>
From: Alice <alice@example.com>
To: bob@example.com
Message-Id: <MiXeD.CaSe@EXAMPLE.com>
Date: Tue, 01 Sep 2026 10:00:00 +0200
Subject: probe
Content-Type: text/plain; charset=utf-8

hello
`

const emlNoID = `From: Alice <alice@example.com>
To: bob@example.com
Date: Tue, 01 Sep 2026 10:00:00 +0200
Subject: no id
Content-Type: text/plain; charset=utf-8

hello
`

func TestMessageIDFromEML(t *testing.T) {
	got, has, err := MessageID([]byte(emlWithID))
	if err != nil {
		t.Fatalf("MessageID: %v", err)
	}
	if !has {
		t.Fatal("MessageID: header present, has=false")
	}
	if got != "mixed.case@example.com" {
		t.Errorf("MessageID = %q, want mixed.case@example.com", got)
	}

	_, has, err = MessageID([]byte(emlNoID))
	if err != nil {
		t.Fatalf("MessageID(no id): %v", err)
	}
	if has {
		t.Fatal("MessageID(no id): has=true, want false")
	}
}

func TestKeyFallbackHash(t *testing.T) {
	k1, has1, err := Key([]byte(emlWithID))
	if err != nil {
		t.Fatalf("Key(with id): %v", err)
	}
	if !has1 || k1 != "mixed.case@example.com" {
		t.Errorf("Key(with id) = %q (has=%v), want mid key", k1, has1)
	}

	k2, has2, err := Key([]byte(emlNoID))
	if err != nil {
		t.Fatalf("Key(no id): %v", err)
	}
	if has2 {
		t.Fatal("Key(no id): has=true, want false (fallback body hash)")
	}
	if !strings.HasPrefix(k2, "body-sha256:") || len(k2) != len("body-sha256:")+64 {
		t.Errorf("Key(no id) = %q, want body-sha256:<hex>", k2)
	}
	// Same body → same fallback key (content addressing for id-less mail).
	k3, _, _ := Key([]byte(emlNoID))
	if k3 != k2 {
		t.Errorf("Key(no id) not stable across calls: %q vs %q", k2, k3)
	}
}

func TestKeyCorruptEML(t *testing.T) {
	if _, _, err := Key([]byte("not a mail at all")); err == nil {
		t.Fatal("Key(corrupt): expected error, got nil")
	}
}

// Regression (live гдеgroup scan, 2026-09-02): 288 corpus messages declare
// iso-8859-15 (legacy German). Header extraction must not fail on charsets
// the MIME stack does not decode — Message-ID needs no charset handling.
func TestMessageIDExoticCharsetHeader(t *testing.T) {
	eml := "From: =?iso-8859-15?Q?Andr=E9?= <andre@example.com>\n" +
		"To: b@example.com\n" +
		"Subject: =?iso-8859-15?Q?Antwort_auf_Ihre_Anfrage?=\n" +
		"Message-Id: <ChArSeT-TeSt@EXAMPLE.com>\n" +
		"Content-Type: text/plain; charset=iso-8859-15\n\n" +
		"Gr\xfc\xdfe\n" // "Grüße" in iso-8859-15
	got, has, err := MessageID([]byte(eml))
	if err != nil {
		t.Fatalf("MessageID(exotic charset): %v", err)
	}
	if !has || got != "charset-test@example.com" {
		t.Errorf("MessageID = %q (has=%v), want charset-test@example.com", got, has)
	}
}
