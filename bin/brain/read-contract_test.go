//go:build brain_readcontract

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Гейт в HTTP-режиме — офлайн против httptest-фикстур (без сети, без Ladybug).

const compliantSearch = `{
	"contract_version": "1.0",
	"query": "matrix federation",
	"root_filter": "",
	"count": 1,
	"results": [{"id": "abc123", "text": "leaf", "root": "facts", "confidence": "confirmed", "score": 0.9}]
}`
const compliantGet = `{"contract_version":"1.0","id":"abc123","root":"facts","confidence":"confirmed","source":"docs","type":"fact"}`
const compliantStats = `{"contract_version":"1.0","total":10,"by_root":{"facts":1,"info":9},"db":"/var/kb.lbug"}`
const compliantAudit = `{"contract_version":"1.0","status":"ok","by_confidence":[{"root":"facts","confidence":"confirmed","count":1}]}`

func newGateServer(t *testing.T, searchBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(searchBody))
	})
	mux.HandleFunc("/get", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(compliantGet))
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(compliantStats))
	})
	mux.HandleFunc("/audit", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(compliantAudit))
	})
	return httptest.NewServer(mux)
}

func TestRunHTTPCompliant(t *testing.T) {
	ts := newGateServer(t, compliantSearch)
	defer ts.Close()
	rep := report{Tool: "test", Mode: "http:" + ts.URL, Version: "1.0"}
	if code := runHTTP(rep, ts.URL, "", "matrix federation", false, false); code != 0 {
		t.Fatalf("compliant service rejected: exit %d", code)
	}
}

func TestRunHTTPRelaxedVersion(t *testing.T) {
	// Сервис до read-контракта: без contract_version, но формат верный.
	old := strings.Replace(compliantSearch, `"contract_version": "1.0",`, ``, 1)
	oldGet := strings.Replace(compliantGet, `"contract_version":"1.0",`, ``, 1)
	oldStats := strings.Replace(compliantStats, `"contract_version":"1.0",`, ``, 1)
	oldAudit := strings.Replace(compliantAudit, `"contract_version":"1.0",`, ``, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(old))
		case "/get":
			w.Write([]byte(oldGet))
		case "/stats":
			w.Write([]byte(oldStats))
		case "/audit":
			w.Write([]byte(oldAudit))
		}
	}))
	defer ts.Close()
	rep := report{Tool: "test", Mode: "http:" + ts.URL, Version: "1.0"}
	// Строго — падает на отсутствии contract_version.
	if code := runHTTP(rep, ts.URL, "", "matrix federation", false, false); code == 0 {
		t.Fatal("pre-contract service must fail strict version check")
	}
	// С --relax-version — проходит (формат верный, версия — warning).
	if code := runHTTP(report{Tool: "test", Mode: "http:" + ts.URL, Version: "1.0"}, ts.URL, "", "matrix federation", true, false); code != 0 {
		t.Fatal("--relax-version must pass on pre-contract service")
	}
}

func TestRunHTTPFormatViolation(t *testing.T) {
	// Нарушение формата: у hit'а нет root, у stats нет by_root — FAIL даже с
	// --relax-version (версия не спасает от неверного формата).
	bad := `{"contract_version":"1.0","query":"q","root_filter":"","count":1,"results":[{"id":"abc","text":"t","score":0.1}]}`
	badStats := `{"contract_version":"1.0","total":5,"db":"/var/kb.lbug"}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(bad))
		case "/get":
			w.Write([]byte(compliantGet))
		case "/stats":
			w.Write([]byte(badStats))
		case "/audit":
			w.Write([]byte(compliantAudit))
		}
	}))
	defer ts.Close()
	rep := report{Tool: "test", Mode: "http:" + ts.URL, Version: "1.0"}
	if code := runHTTP(rep, ts.URL, "", "q", true, false); code == 0 {
		t.Fatal("format violation must fail the gate even with --relax-version")
	}
}

func TestRunHTTPAuthToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer sekret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/search":
			w.Write([]byte(compliantSearch))
		case "/get":
			w.Write([]byte(compliantGet))
		case "/stats":
			w.Write([]byte(compliantStats))
		case "/audit":
			w.Write([]byte(compliantAudit))
		}
	}))
	defer ts.Close()
	rep := report{Tool: "test", Mode: "http:" + ts.URL, Version: "1.0"}
	if code := runHTTP(rep, ts.URL, "sekret", "matrix federation", false, false); code != 0 {
		t.Fatalf("token-authenticated service rejected: exit %d", code)
	}
}
