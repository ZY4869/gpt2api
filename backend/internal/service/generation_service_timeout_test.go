package service

import (
	"testing"
	"time"

	"github.com/kleinai/backend/internal/model"
	"github.com/kleinai/backend/internal/provider"
)

func TestGenerationTimeoutForTask(t *testing.T) {
	tests := []struct {
		name   string
		task   *model.GenerationTask
		params map[string]any
		want   time.Duration
	}{
		{
			name: "default image timeout",
			task: &model.GenerationTask{Kind: string(provider.KindImage), Provider: model.ProviderGPT, ModelCode: "other-model", Count: 1},
			want: 5 * time.Minute,
		},
		{
			name: "video timeout",
			task: &model.GenerationTask{Kind: "video"},
			want: 15 * time.Minute,
		},
		{
			name:   "gpt image2 web count four",
			task:   &model.GenerationTask{Kind: string(provider.KindImage), Provider: model.ProviderGPT, ModelCode: "gpt-image-2", Count: 4},
			params: map[string]any{"resolution": "1K"},
			want:   10 * time.Minute,
		},
		{
			name:   "gpt image2 count over four",
			task:   &model.GenerationTask{Kind: string(provider.KindImage), Provider: model.ProviderGPT, ModelCode: "gpt-image-2", Count: 10},
			params: map[string]any{"resolution": "1K"},
			want:   30 * time.Minute,
		},
		{
			name:   "gpt image2 web wait-all test mode",
			task:   &model.GenerationTask{Kind: string(provider.KindImage), Provider: model.ProviderGPT, ModelCode: "gpt-image-2", Count: 4},
			params: map[string]any{"resolution": "1K", "web_test_mode": "wait_all_then_download"},
			want:   30 * time.Minute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := generationTimeoutForTask(tt.task, tt.params)
			if got != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got)
			}
		})
	}
}

func TestIsTaskStaleAt(t *testing.T) {
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)

	t.Run("wait all mode stale after timeout plus grace", func(t *testing.T) {
		started := now.Add(-(30*time.Minute + staleTaskGrace + time.Second))
		task := &model.GenerationTask{
			Kind:      string(provider.KindImage),
			Provider:  model.ProviderGPT,
			ModelCode: "gpt-image-2",
			Count:     10,
			Status:    model.GenStatusRunning,
			StartedAt: &started,
			Params:    `{"resolution":"1K","web_test_mode":"wait_all_then_download"}`,
		}
		if !isTaskStaleAt(task, now) {
			t.Fatalf("expected task to be stale")
		}
	})

	t.Run("wait all mode not stale before grace", func(t *testing.T) {
		started := now.Add(-(30*time.Minute + staleTaskGrace - time.Second))
		task := &model.GenerationTask{
			Kind:      string(provider.KindImage),
			Provider:  model.ProviderGPT,
			ModelCode: "gpt-image-2",
			Count:     10,
			Status:    model.GenStatusRunning,
			StartedAt: &started,
			Params:    `{"resolution":"1K","web_test_mode":"wait_all_then_download"}`,
		}
		if isTaskStaleAt(task, now) {
			t.Fatalf("expected task not to be stale")
		}
	})

	t.Run("terminal tasks are never stale", func(t *testing.T) {
		started := now.Add(-24 * time.Hour)
		task := &model.GenerationTask{
			Kind:      string(provider.KindImage),
			Provider:  model.ProviderGPT,
			ModelCode: "gpt-image-2",
			Count:     10,
			Status:    model.GenStatusFailed,
			StartedAt: &started,
			Params:    `{"resolution":"1K","web_test_mode":"wait_all_then_download"}`,
		}
		if isTaskStaleAt(task, now) {
			t.Fatalf("terminal task should not be stale")
		}
	})
}
