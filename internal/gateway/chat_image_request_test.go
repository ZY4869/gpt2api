package gateway

import (
	"strings"
	"testing"

	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

func TestPrepareMixedModeRequestUsesPromptCountAndDefaultThinkingEffort(t *testing.T) {
	req, apiErr := prepareMixedModeRequest(
		&modelpkg.Model{Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true},
		mixedModeRequestInput{
			Messages: []chatgpt.ChatMessage{{Role: "user", Content: "根据一个11岁男孩，生成3张连续故事图，透明背景，9:16"}},
		},
		10,
	)
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if req.RequestedN != 3 {
		t.Fatalf("requested_n = %d, want 3", req.RequestedN)
	}
	if !req.WaitForResult {
		t.Fatalf("wait_for_result = %v, want true", req.WaitForResult)
	}
	if req.ThinkingEffort != "standard" {
		t.Fatalf("thinking_effort = %q, want standard", req.ThinkingEffort)
	}
	if !strings.Contains(req.Prompt, "Return exactly 3 separate images") {
		t.Fatalf("prompt = %q, want exact-count instruction", req.Prompt)
	}
	if !strings.Contains(req.Messages[0].Content, "continuous story sequence") {
		t.Fatalf("message prompt = %q, want story-sequence instruction", req.Messages[0].Content)
	}
}

func TestPrepareMixedModeRequestPrefersExplicitNWhenPromptHasNoCount(t *testing.T) {
	n := 4
	waitForResult := false
	req, apiErr := prepareMixedModeRequest(
		&modelpkg.Model{Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true},
		mixedModeRequestInput{
			Messages:       []chatgpt.ChatMessage{{Role: "user", Content: "画一个连续故事书场景"}},
			RequestedN:     &n,
			WaitForResult:  &waitForResult,
			ThinkingEffort: "high",
		},
		10,
	)
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if req.RequestedN != 4 {
		t.Fatalf("requested_n = %d, want 4", req.RequestedN)
	}
	if req.WaitForResult {
		t.Fatalf("wait_for_result = %v, want false", req.WaitForResult)
	}
	if req.ThinkingEffort != "high" {
		t.Fatalf("thinking_effort = %q, want high", req.ThinkingEffort)
	}
	if !strings.Contains(req.Prompt, "Return exactly 4 separate images") {
		t.Fatalf("prompt = %q, want exact-count instruction", req.Prompt)
	}
}

func TestPrepareMixedModeRequestRejectsCountsOutsideAllowedRange(t *testing.T) {
	chatModel := &modelpkg.Model{Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}

	t.Run("over_default_cap", func(t *testing.T) {
		n := 11
		_, apiErr := prepareMixedModeRequest(
			chatModel,
			mixedModeRequestInput{
				Messages:   []chatgpt.ChatMessage{{Role: "user", Content: "生成11张连续故事图"}},
				RequestedN: &n,
			},
			10,
		)
		if apiErr == nil || apiErr.Code != "invalid_request_error" {
			t.Fatalf("apiErr = %+v, want invalid_request_error", apiErr)
		}
	})

	t.Run("over_runtime_cap", func(t *testing.T) {
		n := 3
		_, apiErr := prepareMixedModeRequest(
			chatModel,
			mixedModeRequestInput{
				Messages: []chatgpt.ChatMessage{{
					Role:    "user",
					Content: "生成三张连续故事图",
				}},
				RequestedN: &n,
			},
			2,
		)
		if apiErr == nil || apiErr.Code != "invalid_request_error" {
			t.Fatalf("apiErr = %+v, want invalid_request_error", apiErr)
		}
	})
}

func TestPrepareMixedModeRequestVersionedThinkingModelGetsDefaultEffort(t *testing.T) {
	req, apiErr := prepareMixedModeRequest(
		&modelpkg.Model{Slug: "gpt-5-4-thinking", UpstreamModelSlug: "gpt-5-4-thinking", Type: modelpkg.TypeChat, Enabled: true},
		mixedModeRequestInput{
			Messages: []chatgpt.ChatMessage{{Role: "user", Content: "生成2张连续故事图"}},
		},
		10,
	)
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if req.ThinkingEffort != "standard" {
		t.Fatalf("thinking_effort = %q, want standard", req.ThinkingEffort)
	}
}
