package gateway

import "github.com/432539/gpt2api/internal/upstream/chatgpt"

// ChatCompletionsRequest 对应 OpenAI /v1/chat/completions 请求体子集。
type ChatCompletionsRequest struct {
	Model           string                 `json:"model" binding:"required"`
	Messages        []chatgpt.ChatMessage  `json:"messages" binding:"required"`
	Stream          bool                   `json:"stream"`
	Temperature     float64                `json:"temperature,omitempty"`
	TopP            float64                `json:"top_p,omitempty"`
	MaxTokens       int                    `json:"max_tokens,omitempty"`
	User            string                 `json:"user,omitempty"`
	ImageGeneration bool                   `json:"image_generation,omitempty"`
	Extra           map[string]interface{} `json:"-"`
}

// MixedModeImage 是 chat/responses 共享的图片扩展返回结构。
type MixedModeImage struct {
	URL         string `json:"url"`
	FileID      string `json:"file_id,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	IsPreview   bool   `json:"is_preview,omitempty"`
}

// ChatCompletionResponse 非流式响应。
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   ChatCompletionUsage    `json:"usage"`
	Images  []MixedModeImage       `json:"images,omitempty"`
}

type ChatCompletionChoice struct {
	Index        int                 `json:"index"`
	Message      chatgpt.ChatMessage `json:"message"`
	FinishReason string              `json:"finish_reason"`
}

type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk 流式 chunk。
type ChatCompletionChunk struct {
	ID      string                      `json:"id"`
	Object  string                      `json:"object"`
	Created int64                       `json:"created"`
	Model   string                      `json:"model"`
	Choices []ChatCompletionChunkChoice `json:"choices"`
}

type ChatCompletionChunkChoice struct {
	Index        int      `json:"index"`
	Delta        DeltaMsg `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type DeltaMsg struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}
