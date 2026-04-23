package chatgpt

import "testing"

func TestStreamMessageCollectorCapturesDeltasAndRefs(t *testing.T) {
	collector := NewStreamMessageCollector()

	first := collector.Consume([]byte(`{"v":{"conversation_id":"conv_sse_1","message":{"recipient":"all","content":{"parts":["你好"],"thoughts":[{"summary":"先规划结构","content":"再补充细节"}]}}}}`))
	if first.ConversationID != "conv_sse_1" {
		t.Fatalf("conversation_id = %q, want conv_sse_1", first.ConversationID)
	}
	if first.AssistantDelta != "你好" {
		t.Fatalf("assistant_delta = %q, want 你好", first.AssistantDelta)
	}
	wantReasoning := "先规划结构\n\n再补充细节"
	if first.ReasoningDelta != wantReasoning {
		t.Fatalf("reasoning_delta = %q, want %q", first.ReasoningDelta, wantReasoning)
	}

	second := collector.Consume([]byte(`{"v":[{"p":"/message/content/parts/0","o":"append","v":"，世界 file-service://file_1 sediment://sed_1"},{"p":"/message/content/thoughts/0/content","o":"append","v":"，并校验边界"}]}`))
	if second.AssistantDelta != "，世界" {
		t.Fatalf("assistant_delta = %q, want ，世界", second.AssistantDelta)
	}
	if second.ReasoningDelta != "，并校验边界" {
		t.Fatalf("reasoning_delta = %q, want ，并校验边界", second.ReasoningDelta)
	}
	if len(second.FileIDs) != 1 || second.FileIDs[0] != "file_1" {
		t.Fatalf("file_ids = %#v, want [file_1]", second.FileIDs)
	}
	if len(second.SedimentIDs) != 1 || second.SedimentIDs[0] != "sed_1" {
		t.Fatalf("sediment_ids = %#v, want [sed_1]", second.SedimentIDs)
	}

	final := collector.Consume([]byte(`[DONE]`))
	if !final.Final {
		t.Fatalf("final = false, want true")
	}
	if final.AssistantText != "你好，世界" {
		t.Fatalf("assistant_text = %q, want 你好，世界", final.AssistantText)
	}
	wantFinalReasoning := "先规划结构\n\n再补充细节，并校验边界"
	if final.ReasoningText != wantFinalReasoning {
		t.Fatalf("reasoning_text = %q, want %q", final.ReasoningText, wantFinalReasoning)
	}
	if !final.ThinkingTriggered {
		t.Fatal("thinking_triggered = false, want true")
	}
	if final.ThinkingTriggeredVia != "reasoning_text" {
		t.Fatalf("thinking_triggered_via = %q, want reasoning_text", final.ThinkingTriggeredVia)
	}
}

func TestStreamMessageCollectorMarksThoughtPatchWithoutVisibleReasoning(t *testing.T) {
	collector := NewStreamMessageCollector()

	update := collector.Consume([]byte(`{"v":[{"p":"/message/content/thoughts/0/summary","o":"append","v":""}]}`))
	if !update.ThinkingTriggered {
		t.Fatal("thinking_triggered = false, want true")
	}
	if update.ThinkingTriggeredVia != "thought_patch" {
		t.Fatalf("thinking_triggered_via = %q, want thought_patch", update.ThinkingTriggeredVia)
	}
	if !update.SawThoughtPatch {
		t.Fatal("saw_thought_patch = false, want true")
	}
	if update.ReasoningText != "" {
		t.Fatalf("reasoning_text = %q, want empty", update.ReasoningText)
	}
}

func TestStreamMessageCollectorMarksThinkingMetadataWithoutVisibleReasoning(t *testing.T) {
	collector := NewStreamMessageCollector()

	update := collector.Consume([]byte(`{"message":{"metadata":{"ghostrider_status":"running","reasoning_start_time":"1710806400"}}}`))
	if !update.ThinkingTriggered {
		t.Fatal("thinking_triggered = false, want true")
	}
	if update.ThinkingTriggeredVia != "metadata" {
		t.Fatalf("thinking_triggered_via = %q, want metadata", update.ThinkingTriggeredVia)
	}
	if update.SawThoughtPatch {
		t.Fatal("saw_thought_patch = true, want false")
	}
	if !update.SawThinkingMetadata {
		t.Fatal("saw_thinking_metadata = false, want true")
	}
	if update.ReasoningText != "" {
		t.Fatalf("reasoning_text = %q, want empty", update.ReasoningText)
	}
}
