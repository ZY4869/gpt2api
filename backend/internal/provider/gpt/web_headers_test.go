package gpt

import "testing"

func TestWebBaseHeadersIncludeCookieAndUserAgent(t *testing.T) {
	fp := newWebFP("Mozilla/5.0 SolverUA")
	headers := webBaseHeaders(fp, "token-1", "/backend-api/test", "cf_clearance=abc; session=xyz")

	if headers["User-Agent"] != "Mozilla/5.0 SolverUA" {
		t.Fatalf("expected solver user-agent, got %q", headers["User-Agent"])
	}
	if headers["Cookie"] != "cf_clearance=abc; session=xyz" {
		t.Fatalf("expected solver cookies, got %q", headers["Cookie"])
	}
	if headers["Authorization"] != "Bearer token-1" {
		t.Fatalf("expected bearer token, got %q", headers["Authorization"])
	}
}

func TestNewWebFPKeepsDefaultUserAgent(t *testing.T) {
	fp := newWebFP("")
	if fp.UserAgent == "" {
		t.Fatalf("expected default user-agent")
	}
}
