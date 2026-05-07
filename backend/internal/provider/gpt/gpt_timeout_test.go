package gpt

import (
	"context"
	"testing"
	"time"
)

func TestWebImagePollDeadlineUsesMaxWindowWithoutContextDeadline(t *testing.T) {
	before := time.Now().Add(9*time.Minute - 2*time.Second)
	got := webImagePollDeadline(context.Background(), 9*time.Minute, 15*time.Second)
	after := time.Now().Add(9*time.Minute + 2*time.Second)
	if got.Before(before) || got.After(after) {
		t.Fatalf("expected deadline near 9m from now, got %s", got)
	}
}

func TestWebImagePollDeadlineHonorsContextDeadlineSafetyMargin(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(4*time.Minute))
	defer cancel()
	got := webImagePollDeadline(ctx, 9*time.Minute, 15*time.Second)
	wantLower := time.Now().Add(4*time.Minute - 17*time.Second)
	wantUpper := time.Now().Add(4*time.Minute - 13*time.Second)
	if got.Before(wantLower) || got.After(wantUpper) {
		t.Fatalf("expected deadline near ctx deadline minus safety margin, got %s", got)
	}
}
