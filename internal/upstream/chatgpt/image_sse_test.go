package chatgpt

import "testing"

func TestParseImageSSECapturesReasoningAssistantAndFiles(t *testing.T) {
	stream := make(chan SSEEvent, 4)
	stream <- SSEEvent{Data: []byte(`{"v":{"conversation_id":"conv_reasoning","message":{"recipient":"all","content":{"parts":[""],"thoughts":[{"summary":"先规划故事线","content":"确定男孩从起床到登山作画的连续动作"}]},"metadata":{"image_gen_task_id":"img_task_1","finish_details":{"type":"stop"}}}}}`)}
	stream <- SSEEvent{Data: []byte(`{"v":[{"p":"/message/content/thoughts/0/summary","o":"append","v":"，再统一画风"},{"p":"/message/content/thoughts/0/content","o":"append","v":"，最后把山顶作画放在结尾"},{"p":"/message/content/parts/0","o":"append","v":"我会生成两张连续故事图，保持透明背景和9:16比例。"},{"p":"/message/content/parts/0","o":"append","v":" file-service://file_a sediment://sed_1"}]}`)}
	stream <- SSEEvent{Data: []byte(`[DONE]`)}
	close(stream)

	got := ParseImageSSE(stream)

	if got.ConversationID != "conv_reasoning" {
		t.Fatalf("conversation_id = %q, want conv_reasoning", got.ConversationID)
	}
	if got.ImageGenTaskID != "img_task_1" {
		t.Fatalf("image_gen_task_id = %q, want img_task_1", got.ImageGenTaskID)
	}
	if got.FinishType != "stop" {
		t.Fatalf("finish_type = %q, want stop", got.FinishType)
	}
	if len(got.FileIDs) != 1 || got.FileIDs[0] != "file_a" {
		t.Fatalf("file_ids = %#v, want [file_a]", got.FileIDs)
	}
	if len(got.SedimentIDs) != 1 || got.SedimentIDs[0] != "sed_1" {
		t.Fatalf("sediment_ids = %#v, want [sed_1]", got.SedimentIDs)
	}
	wantReasoning := "先规划故事线，再统一画风\n\n确定男孩从起床到登山作画的连续动作，最后把山顶作画放在结尾"
	if got.ReasoningText != wantReasoning {
		t.Fatalf("reasoning_text = %q, want %q", got.ReasoningText, wantReasoning)
	}
	wantAssistant := "我会生成两张连续故事图，保持透明背景和9:16比例。"
	if got.AssistantText != wantAssistant {
		t.Fatalf("assistant_text = %q, want %q", got.AssistantText, wantAssistant)
	}
}

func TestParseImageSSEUsesFullMessageFallback(t *testing.T) {
	stream := make(chan SSEEvent, 2)
	stream <- SSEEvent{Data: []byte(`{"message":{"recipient":"all","content":{"parts":["已整理为两张连续故事图"],"thoughts":[{"summary":"保留角色一致性","content":"让动作和时间线自然衔接"}]}}}`)}
	close(stream)

	got := ParseImageSSE(stream)

	if got.AssistantText != "已整理为两张连续故事图" {
		t.Fatalf("assistant_text = %q, want 已整理为两张连续故事图", got.AssistantText)
	}
	wantReasoning := "保留角色一致性\n\n让动作和时间线自然衔接"
	if got.ReasoningText != wantReasoning {
		t.Fatalf("reasoning_text = %q, want %q", got.ReasoningText, wantReasoning)
	}
}
