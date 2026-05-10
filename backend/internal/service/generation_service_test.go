package service

import (
	"errors"
	"testing"
	"time"
)

func TestProviderCooldownGrokForbiddenIsTransient(t *testing.T) {
	err := errors.New(`grok upload HTTP 403: <!DOCTYPE html><html><head><title>Just a moment...</title></head></html>`)
	if got := providerCooldown(err); got != 0 {
		t.Fatalf("expected transient cooldown 0, got %s", got)
	}
}

func TestProviderCooldownRetryable429StillCooldowns(t *testing.T) {
	err := errors.New(`provider call: grok video HTTP 429: {"error":{"code":8,"message":"Too many requests"}}`)
	got := providerCooldown(err)
	if got < 30*time.Minute {
		t.Fatalf("expected 429 cooldown >= 30m, got %s", got)
	}
}

func TestRetryableProviderErrorTreatsTransientNetworkAsRetryable(t *testing.T) {
	cases := []error{
		errors.New("unexpected EOF"),
		errors.New("read tcp 127.0.0.1:1234->127.0.0.1:443: connection reset by peer"),
		errors.New("write tcp 127.0.0.1:1234->127.0.0.1:443: broken pipe"),
		errors.New("dial tcp 127.0.0.1:443: connectex: connection refused"),
	}
	for _, err := range cases {
		if !retryableProviderError(err) {
			t.Fatalf("expected transient network error to be retryable: %v", err)
		}
	}
}

func TestIsTransientProviderPathErrorTreatsTransientNetworkAsTransient(t *testing.T) {
	err := errors.New("read tcp 127.0.0.1:1234->127.0.0.1:443: connection reset by peer")
	if !isTransientProviderPathError("", err) {
		t.Fatalf("expected network reset to be treated as transient path error")
	}
}

func TestActualGenerationCostUsesActualResultCount(t *testing.T) {
	got := actualGenerationCost(1000, 10, 8)
	if got != 800 {
		t.Fatalf("expected cost for 8 actual results, got %d", got)
	}
}
