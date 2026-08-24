package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sample struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestGetJSONDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(sample{Name: "alice", Count: 3})
	}))
	defer srv.Close()

	var got sample
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "alice" || got.Count != 3 {
		t.Fatalf("got %+v", got)
	}
}

func TestDoJSONErrorIncludesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not loaded"}`, http.StatusBadGateway)
	}))
	defer srv.Close()

	err := GetJSON(context.Background(), srv.Client(), srv.URL, &sample{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "model not loaded") {
		t.Fatalf("err = %v", err)
	}
}

func TestDoJSONSendsRequestAndContentType(t *testing.T) {
	var sawMethod, sawCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawMethod = r.Method
		sawCT = r.Header.Get("Content-Type")
		json.NewEncoder(w).Encode(sample{Name: "bob"})
	}))
	defer srv.Close()

	req := sample{Name: "request"}
	var got sample
	if err := DoJSON(context.Background(), srv.Client(), http.MethodPost, srv.URL, req, &got); err != nil {
		t.Fatal(err)
	}
	if sawMethod != http.MethodPost || sawCT != "application/json" {
		t.Fatalf("method=%s ct=%q", sawMethod, sawCT)
	}
}

func TestDoJSONOptsSetHeaders(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"name":"x","count":1}`))
	}))
	defer srv.Close()

	var got sample
	opt := func(req *http.Request) { req.Header.Set("Authorization", "Basic dTpw") }
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, &got, opt); err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Basic dTpw" {
		t.Fatalf("auth = %q", sawAuth)
	}
}

func TestGetJSONNilClientUsesDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"n","count":0}`))
	}))
	defer srv.Close()

	var got sample
	if err := GetJSON(context.Background(), nil, srv.URL, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "n" {
		t.Fatalf("got %+v", got)
	}
}
