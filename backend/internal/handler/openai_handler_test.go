package handler

import (
	"testing"

	"github.com/kleinai/backend/internal/model"
)

func TestResolveImageCount(t *testing.T) {
	tests := []struct {
		name    string
		n       int
		count   int
		want    int
		wantErr string
	}{
		{name: "default one", want: 1},
		{name: "prefer n", n: 10, count: 2, want: 10},
		{name: "fallback count", count: 10, want: 10},
		{name: "reject over limit by n", n: 11, wantErr: "n/count must be less than or equal to 10"},
		{name: "reject over limit by count", count: 11, wantErr: "n/count must be less than or equal to 10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveImageCount(tt.n, tt.count)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestNormalizeOpenAIResultURLForCachedImage(t *testing.T) {
	task := &model.GenerationTask{TaskID: "task_123", Kind: "image"}
	result := &model.GenerationResult{URL: "/api/v1/gen/cached/generated/2026/05/07/task_123_0.png"}
	got := normalizeOpenAIResultURL("http://127.0.0.1:17200", task, result, 0, false)
	want := "http://127.0.0.1:17200/v1/files/task_123/0"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestNormalizeOpenAIResultURLForRemoteImage(t *testing.T) {
	task := &model.GenerationTask{TaskID: "task_123", Kind: "image"}
	result := &model.GenerationResult{URL: "https://example.com/out.png"}
	got := normalizeOpenAIResultURL("http://127.0.0.1:17200", task, result, 0, false)
	want := "https://example.com/out.png"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
