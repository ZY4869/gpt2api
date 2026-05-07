package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCFSolverServiceSolveSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status":"ok",
			"solution":{
				"userAgent":"Mozilla/5.0 Test",
				"cookies":[
					{"name":"cf_clearance","value":"clear"},
					{"name":"__Secure-next-auth.session-token","value":"sess"}
				]
			}
		}`))
	}))
	defer srv.Close()

	solver := NewCFSolverService()
	got, err := solver.Solve(context.Background(), srv.URL, "https://chatgpt.com/", "", 30*time.Second)
	if err != nil {
		t.Fatalf("Solve() error = %v", err)
	}
	if got == nil {
		t.Fatalf("Solve() returned nil result")
	}
	if got.CFClearance != "clear" {
		t.Fatalf("expected cf_clearance, got %q", got.CFClearance)
	}
	if !strings.Contains(got.Cookies, "cf_clearance=clear") || !strings.Contains(got.Cookies, "__Secure-next-auth.session-token=sess") {
		t.Fatalf("unexpected cookies %q", got.Cookies)
	}
	if got.UserAgent != "Mozilla/5.0 Test" {
		t.Fatalf("unexpected user-agent %q", got.UserAgent)
	}
}

func TestCFSolverServiceSolveHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	solver := NewCFSolverService()
	_, err := solver.Solve(context.Background(), srv.URL, "https://chatgpt.com/", "", 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), "flaresolverr HTTP 502") {
		t.Fatalf("expected HTTP error, got %v", err)
	}
}

func TestCFSolverServiceSolveStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"error","message":"challenge failed"}`))
	}))
	defer srv.Close()

	solver := NewCFSolverService()
	_, err := solver.Solve(context.Background(), srv.URL, "https://chatgpt.com/", "", 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), `flaresolverr status "error"`) {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestCFSolverServiceSolveMissingCookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","solution":{"userAgent":"Mozilla/5.0","cookies":[]}}`))
	}))
	defer srv.Close()

	solver := NewCFSolverService()
	_, err := solver.Solve(context.Background(), srv.URL, "https://chatgpt.com/", "", 30*time.Second)
	if err == nil || !strings.Contains(err.Error(), "returned no cookies") {
		t.Fatalf("expected missing cookies error, got %v", err)
	}
}
