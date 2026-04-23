package chatgpt

import "testing"

func TestSSETextCollectorCapturesThoughtSignals(t *testing.T) {
	collector := newSSETextCollector()
	collector.Consume([]byte(`{"message":{"recipient":"all","content":{"thoughts":[{"summary":"先规划分镜","content":"再统一画风"}]}}}`))

	got := collector.Result()
	wantReasoning := "先规划分镜\n\n再统一画风"
	if got.ReasoningText != wantReasoning {
		t.Fatalf("reasoning_text = %q, want %q", got.ReasoningText, wantReasoning)
	}
	if !got.SawThoughtPatch {
		t.Fatal("saw_thought_patch = false, want true")
	}
	if got.SawThinkingMetadata {
		t.Fatal("saw_thinking_metadata = true, want false")
	}
}

func TestSSETextCollectorMarksEmptyThoughtPatchAsThinkingSignal(t *testing.T) {
	collector := newSSETextCollector()
	collector.Consume([]byte(`{"v":[{"p":"/message/content/thoughts/0/summary","o":"append","v":""}]}`))

	got := collector.Result()
	if got.ReasoningText != "" {
		t.Fatalf("reasoning_text = %q, want empty", got.ReasoningText)
	}
	if !got.SawThoughtPatch {
		t.Fatal("saw_thought_patch = false, want true")
	}
	if got.SawThinkingMetadata {
		t.Fatal("saw_thinking_metadata = true, want false")
	}
}

func TestSSETextCollectorMarksThinkingMetadataWithoutVisibleReasoning(t *testing.T) {
	collector := newSSETextCollector()
	collector.Consume([]byte(`{"message":{"metadata":{"ghostrider_status":"running","async_task_title":"生成图片"}}}`))

	got := collector.Result()
	if got.ReasoningText != "" {
		t.Fatalf("reasoning_text = %q, want empty", got.ReasoningText)
	}
	if got.SawThoughtPatch {
		t.Fatal("saw_thought_patch = true, want false")
	}
	if !got.SawThinkingMetadata {
		t.Fatal("saw_thinking_metadata = false, want true")
	}
}
