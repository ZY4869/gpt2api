package chatgpt

import "testing"

func TestSSETextCollectorCapturesThoughtText(t *testing.T) {
	collector := newSSETextCollector()
	collector.Consume([]byte(`{"message":{"recipient":"all","content":{"thoughts":[{"summary":"先规划分镜","content":"再统一画风"}]}}}`))

	got := collector.Result()
	wantReasoning := "先规划分镜\n\n再统一画风"
	if got.ReasoningText != wantReasoning {
		t.Fatalf("reasoning_text = %q, want %q", got.ReasoningText, wantReasoning)
	}
}

func TestSSETextCollectorIgnoresEmptyThoughtPatch(t *testing.T) {
	collector := newSSETextCollector()
	collector.Consume([]byte(`{"v":[{"p":"/message/content/thoughts/0/summary","o":"append","v":""}]}`))

	got := collector.Result()
	if got.ReasoningText != "" {
		t.Fatalf("reasoning_text = %q, want empty", got.ReasoningText)
	}
}
