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

func TestNewWebFPAlignsSecCHUAWithChromeSolverUA(t *testing.T) {
	fp := newWebFP("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	if fp.SecCHUA != `"Google Chrome";v="136", "Chromium";v="136", "Not_A Brand";v="24"` {
		t.Fatalf("unexpected sec-ch-ua %q", fp.SecCHUA)
	}
	if fp.Platform != "Windows" {
		t.Fatalf("unexpected platform %q", fp.Platform)
	}
}

func TestWebBootstrapHeadersLookLikeNavigation(t *testing.T) {
	fp := newWebFP("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	headers := webBootstrapHeaders(fp, "cf_clearance=abc")

	if headers["Sec-Fetch-Mode"] != "navigate" {
		t.Fatalf("expected navigate mode, got %q", headers["Sec-Fetch-Mode"])
	}
	if headers["Sec-Fetch-Site"] != "none" {
		t.Fatalf("expected top-level navigation site, got %q", headers["Sec-Fetch-Site"])
	}
	if headers["Sec-Fetch-Dest"] != "document" {
		t.Fatalf("expected document dest, got %q", headers["Sec-Fetch-Dest"])
	}
	if headers["Sec-Fetch-User"] != "?1" {
		t.Fatalf("expected fetch user, got %q", headers["Sec-Fetch-User"])
	}
	if headers["Upgrade-Insecure-Requests"] != "1" {
		t.Fatalf("expected upgrade insecure requests, got %q", headers["Upgrade-Insecure-Requests"])
	}
	if headers["Cookie"] != "cf_clearance=abc" {
		t.Fatalf("expected bootstrap cookie, got %q", headers["Cookie"])
	}
	if _, ok := headers["Authorization"]; ok {
		t.Fatalf("bootstrap headers should not include authorization")
	}
	if _, ok := headers["X-OpenAI-Target-Path"]; ok {
		t.Fatalf("bootstrap headers should not include target path")
	}
}
