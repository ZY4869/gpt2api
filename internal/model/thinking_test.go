package model

import "testing"

func TestResolveThinkingModel(t *testing.T) {
	tests := []struct {
		name                 string
		model                *Model
		wantThinking         bool
		wantCanonical        string
		wantResolvedUpstream string
	}{
		{
			name: "stable_alias",
			model: &Model{
				Slug:              StableThinkingSlug,
				UpstreamModelSlug: DefaultThinkingUpstreamSlug,
			},
			wantThinking:         true,
			wantCanonical:        StableThinkingSlug,
			wantResolvedUpstream: DefaultThinkingUpstreamSlug,
		},
		{
			name: "versioned_slug",
			model: &Model{
				Slug:              "gpt-5-2-thinking",
				UpstreamModelSlug: "gpt-5-2-thinking",
			},
			wantThinking:         true,
			wantCanonical:        "gpt-5-2-thinking",
			wantResolvedUpstream: "gpt-5-2-thinking",
		},
		{
			name: "fallback_upstream_for_alias",
			model: &Model{
				Slug: StableThinkingSlug,
			},
			wantThinking:         true,
			wantCanonical:        StableThinkingSlug,
			wantResolvedUpstream: DefaultThinkingUpstreamSlug,
		},
		{
			name: "plain_chat_model",
			model: &Model{
				Slug:              "gpt-5",
				UpstreamModelSlug: "gpt-5-3",
			},
			wantCanonical: "gpt-5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveThinkingModel(tc.model)
			if got.IsThinking != tc.wantThinking {
				t.Fatalf("IsThinking = %v, want %v", got.IsThinking, tc.wantThinking)
			}
			if got.CanonicalSlug != tc.wantCanonical {
				t.Fatalf("CanonicalSlug = %q, want %q", got.CanonicalSlug, tc.wantCanonical)
			}
			if got.ResolvedUpstreamSlug != tc.wantResolvedUpstream {
				t.Fatalf("ResolvedUpstreamSlug = %q, want %q", got.ResolvedUpstreamSlug, tc.wantResolvedUpstream)
			}
		})
	}
}

func TestBuildPublicModelsIncludesThinkingFlag(t *testing.T) {
	got := BuildPublicModels([]*Model{
		{ID: 1, Slug: "gpt-5", Type: TypeChat, Description: "plain"},
		{ID: 2, Slug: StableThinkingSlug, Type: TypeChat, Description: "thinking"},
	})

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].IsThinking {
		t.Fatalf("first model IsThinking = true, want false")
	}
	if !got[1].IsThinking {
		t.Fatalf("second model IsThinking = false, want true")
	}
}
