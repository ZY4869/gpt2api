package gateway

import (
	"strings"
	"testing"

	modelpkg "github.com/432539/gpt2api/internal/model"
)

func TestResolveRequestedUpstreamModel(t *testing.T) {
	tests := []struct {
		name  string
		model *modelpkg.Model
		want  string
	}{
		{
			name:  "stable_alias",
			model: &modelpkg.Model{Slug: modelpkg.StableThinkingSlug},
			want:  modelpkg.DefaultThinkingUpstreamSlug,
		},
		{
			name:  "versioned_thinking",
			model: &modelpkg.Model{Slug: "gpt-5-4-thinking", UpstreamModelSlug: "gpt-5-4-thinking"},
			want:  "gpt-5-4-thinking",
		},
		{
			name:  "plain_model_uses_mapping",
			model: &modelpkg.Model{Slug: "gpt-5", UpstreamModelSlug: "gpt-5"},
			want:  "gpt-5-3",
		},
		{
			name:  "plain_model_without_upstream_falls_back_to_auto",
			model: &modelpkg.Model{Slug: "gpt-4o"},
			want:  "auto",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveRequestedUpstreamModel(tc.model); got != tc.want {
				t.Fatalf("resolveRequestedUpstreamModel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestModelNotFoundMessageForThinkingSlug(t *testing.T) {
	msg := modelNotFoundMessage("gpt-5-thinking")
	for _, slug := range modelpkg.SupportedThinkingSlugs() {
		if !strings.Contains(msg, slug) {
			t.Fatalf("message = %q, want mention %q", msg, slug)
		}
	}
}
