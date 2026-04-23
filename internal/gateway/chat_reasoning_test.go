package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

func makeThinkingChatSSE() <-chan chatgpt.SSEEvent {
	stream := make(chan chatgpt.SSEEvent, 2)
	stream <- chatgpt.SSEEvent{Data: []byte(`{"message":{"recipient":"all","content":{"parts":["你好，世界"],"thoughts":[{"summary":"先想清楚结构","content":"再组织最终回答"}]}}}`)}
	stream <- chatgpt.SSEEvent{Data: []byte(`[DONE]`)}
	close(stream)
	return stream
}

func TestStreamOpenAIIncludesReasoningDelta(t *testing.T) {
	h := &Handler{}
	c, w := newJSONContext(t, "/v1/chat/completions", `{}`, nil)

	h.streamOpenAI(c, "chatcmpl-thinking-stream", "gpt-5-thinking", makeThinkingChatSSE(), false, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`"reasoning":"先想清楚结构\n\n再组织最终回答"`,
		`"content":"你好，世界"`,
		`data: [DONE]`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("stream body missing %q:\n%s", want, body)
		}
	}
	if got, ok := c.Get("reasoning_text"); !ok || got != "先想清楚结构\n\n再组织最终回答" {
		t.Fatalf("reasoning_text = %#v, want thinking summary", got)
	}
}

func TestCollectOpenAIReturnsReasoningField(t *testing.T) {
	h := &Handler{}
	c, w := newJSONContext(t, "/v1/chat/completions", `{}`, nil)

	h.collectOpenAI(c, "chatcmpl-thinking-nonstream", "gpt-5-thinking", makeThinkingChatSSE(), false, true)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp ChatCompletionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("choices = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Content != "你好，世界" {
		t.Fatalf("message.content = %q, want 你好，世界", resp.Choices[0].Message.Content)
	}
	if resp.Choices[0].Reasoning != "先想清楚结构\n\n再组织最终回答" {
		t.Fatalf("reasoning = %q, want thinking summary", resp.Choices[0].Reasoning)
	}
	if got, ok := c.Get("reasoning_text"); !ok || got != "先想清楚结构\n\n再组织最终回答" {
		t.Fatalf("reasoning_text = %#v, want thinking summary", got)
	}
}
