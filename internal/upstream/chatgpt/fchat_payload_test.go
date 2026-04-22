package chatgpt

import "testing"

func TestBuildFChatPayloadPictureV2Thinking(t *testing.T) {
	opt := FChatOpts{
		UpstreamModel:  "gpt-5-4-thinking",
		Messages:       []ChatMessage{{Role: "user", Content: "画三张连续故事图"}},
		ParentMsgID:    "parent-1",
		ThinkingEffort: "standard",
		ImageStrategy:  ImageStrategyPictureV2Thinking,
	}

	prep := buildFChatPreparePayload(opt)
	if got := prep["thinking_effort"]; got != "standard" {
		t.Fatalf("prepare thinking_effort = %#v, want standard", got)
	}
	if got := prep["system_hints"]; len(got.([]string)) != 1 || got.([]string)[0] != "picture_v2" {
		t.Fatalf("prepare system_hints = %#v, want picture_v2", got)
	}

	stream := buildFChatStreamPayload(opt)
	if got := stream["thinking_effort"]; got != "standard" {
		t.Fatalf("stream thinking_effort = %#v, want standard", got)
	}
	if got := stream["system_hints"]; len(got.([]string)) != 1 || got.([]string)[0] != "picture_v2" {
		t.Fatalf("stream system_hints = %#v, want picture_v2", got)
	}
	msgs := stream["messages"].([]map[string]interface{})
	meta := msgs[0]["metadata"].(map[string]interface{})
	if _, ok := meta["selected_sources"]; ok {
		t.Fatalf("picture_v2 metadata should not contain selected_sources: %#v", meta)
	}
	if got := meta["system_hints"]; len(got.([]string)) != 1 || got.([]string)[0] != "picture_v2" {
		t.Fatalf("message metadata system_hints = %#v, want picture_v2", got)
	}
}

func TestBuildFChatPayloadNativeThinking(t *testing.T) {
	opt := FChatOpts{
		UpstreamModel:  "gpt-5-4-thinking",
		Messages:       []ChatMessage{{Role: "user", Content: "画三张连续故事图"}},
		ParentMsgID:    "parent-1",
		ThinkingEffort: "standard",
		ImageStrategy:  ImageStrategyNativeThinking,
	}

	prep := buildFChatPreparePayload(opt)
	if got := prep["thinking_effort"]; got != "standard" {
		t.Fatalf("prepare thinking_effort = %#v, want standard", got)
	}
	if got := prep["system_hints"]; len(got.([]string)) != 0 {
		t.Fatalf("prepare system_hints = %#v, want empty", got)
	}

	stream := buildFChatStreamPayload(opt)
	if got := stream["thinking_effort"]; got != "standard" {
		t.Fatalf("stream thinking_effort = %#v, want standard", got)
	}
	if got := stream["system_hints"]; len(got.([]string)) != 0 {
		t.Fatalf("stream system_hints = %#v, want empty", got)
	}
	msgs := stream["messages"].([]map[string]interface{})
	meta := msgs[0]["metadata"].(map[string]interface{})
	if _, ok := meta["system_hints"]; ok {
		t.Fatalf("native thinking metadata should not contain system_hints: %#v", meta)
	}
	if got := meta["selected_sources"].([]interface{}); len(got) != 0 {
		t.Fatalf("selected_sources = %#v, want empty", got)
	}
}
