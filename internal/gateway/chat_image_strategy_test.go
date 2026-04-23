package gateway

import (
	"testing"

	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

func TestMixedModeImageStrategyDefaultsThinkingToNative(t *testing.T) {
	h := &Handler{}
	thinkingModel := &modelpkg.Model{Slug: "gpt-5-thinking", Type: modelpkg.TypeChat}
	plainModel := &modelpkg.Model{Slug: "gpt-5", Type: modelpkg.TypeChat}

	if got := h.mixedModeImageStrategy(thinkingModel); got != chatgpt.ImageStrategyNativeThinking {
		t.Fatalf("thinking strategy = %q, want %q", got, chatgpt.ImageStrategyNativeThinking)
	}
	if got := h.mixedModeImageStrategy(plainModel); got != chatgpt.ImageStrategyPictureV2Thinking {
		t.Fatalf("plain strategy = %q, want %q", got, chatgpt.ImageStrategyPictureV2Thinking)
	}

	h.Settings = fakeSettings{thinkingStrategy: chatgpt.ImageStrategyPictureV2Thinking}
	if got := h.mixedModeImageStrategy(thinkingModel); got != chatgpt.ImageStrategyPictureV2Thinking {
		t.Fatalf("configured strategy = %q, want %q", got, chatgpt.ImageStrategyPictureV2Thinking)
	}
}

func TestMixedModeThinkingSignals(t *testing.T) {
	cases := []struct {
		name string
		res  mixedModeExecResult
		want mixedModeThinkingSignals
	}{
		{
			name: "reasoning_text",
			res:  mixedModeExecResult{ReasoningText: "先规划角色动作。"},
			want: mixedModeThinkingSignals{Triggered: true, TriggeredVia: mixedModeThinkingViaReasoningText, ReasoningEmpty: false, ReasoningLen: len("先规划角色动作。")},
		},
		{
			name: "thought_patch_only",
			res:  mixedModeExecResult{SawThoughtPatch: true},
			want: mixedModeThinkingSignals{Triggered: true, TriggeredVia: mixedModeThinkingViaThoughtPatch, SawThoughtPatch: true, ReasoningEmpty: true},
		},
		{
			name: "metadata_only",
			res:  mixedModeExecResult{ThinkingTriggered: true, ThinkingTriggeredVia: mixedModeThinkingViaMetadata, SawThinkingMetadata: true},
			want: mixedModeThinkingSignals{Triggered: true, TriggeredVia: mixedModeThinkingViaMetadata, SawThinkingMetadata: true, ReasoningEmpty: true},
		},
		{
			name: "none",
			res:  mixedModeExecResult{},
			want: mixedModeThinkingSignals{Triggered: false, ReasoningEmpty: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.res.thinkingSignals()
			if got != tc.want {
				t.Fatalf("thinking signals = %#v, want %#v", got, tc.want)
			}
		})
	}
}
