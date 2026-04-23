package image

import (
	"context"
	"errors"
	"testing"

	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

func TestRetryableErrorCode(t *testing.T) {
	if !retryableErrorCode(ErrPreviewOnly) {
		t.Fatal("preview_only should be retryable")
	}
	if !retryableErrorCode(ErrNetworkTransient) {
		t.Fatal("network_transient should be retryable")
	}
	if retryableErrorCode(ErrUpstream) {
		t.Fatal("upstream_error should not be retryable")
	}
}

func TestClassifyUpstream(t *testing.T) {
	runner := &Runner{}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "rate_limit", err: &chatgpt.UpstreamError{Status: 429, Message: "rate limited"}, want: ErrRateLimited},
		{name: "unauthorized", err: &chatgpt.UpstreamError{Status: 401, Message: "unauthorized"}, want: ErrAuthRequired},
		{name: "deadline", err: context.DeadlineExceeded, want: ErrPollTimeout},
		{name: "eof", err: errors.New("unexpected EOF"), want: ErrNetworkTransient},
		{name: "reset", err: errors.New("read tcp: connection reset by peer"), want: ErrNetworkTransient},
		{name: "upstream", err: errors.New("boom"), want: ErrUpstream},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := runner.classifyUpstream(tc.err); got != tc.want {
				t.Fatalf("classifyUpstream() = %q, want %q", got, tc.want)
			}
		})
	}
}
