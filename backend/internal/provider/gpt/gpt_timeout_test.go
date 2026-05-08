package gpt

import (
	"context"
	"testing"
	"time"

	"github.com/kleinai/backend/internal/provider"
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

func TestWebImagePollDeadlineUsesThirtyMinutesForWaitAllTestMode(t *testing.T) {
	req := &provider.Request{
		ModelCode: "gpt-image-2",
		Count:     10,
		Params:    map[string]any{"web_test_mode": "wait_all_then_download"},
	}
	mode := webImageTestMode(req)
	if !mode.Enabled {
		t.Fatalf("expected test mode enabled")
	}

	pollWindow := 9 * time.Minute
	if mode.Enabled {
		pollWindow = 30 * time.Minute
	}
	before := time.Now().Add(30*time.Minute - 2*time.Second)
	got := webImagePollDeadline(context.Background(), pollWindow, 15*time.Second)
	after := time.Now().Add(30*time.Minute + 2*time.Second)
	if got.Before(before) || got.After(after) {
		t.Fatalf("expected deadline near 30m from now, got %s", got)
	}
}

func TestWebImagePollStepContextUsesShortTimeout(t *testing.T) {
	start := time.Now()
	ctx := webImagePollStepContext(context.Background())
	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatalf("expected poll step context deadline")
	}
	lower := start.Add(webImagePollStepTimeout - 2*time.Second)
	upper := start.Add(webImagePollStepTimeout + 2*time.Second)
	if dl.Before(lower) || dl.After(upper) {
		t.Fatalf("expected deadline near short step timeout, got %s", dl)
	}
}
