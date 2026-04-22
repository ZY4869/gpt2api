package gateway

import "encoding/json"

// ResponseCreateRequest 是 /v1/responses 的首版子集。
type ResponseCreateRequest struct {
	Model           string            `json:"model" binding:"required"`
	Input           json.RawMessage   `json:"input" binding:"required"`
	Stream          bool              `json:"stream"`
	Temperature     float64           `json:"temperature,omitempty"`
	User            string            `json:"user,omitempty"`
	Instructions    string            `json:"instructions,omitempty"`
	ImageGeneration bool              `json:"image_generation,omitempty"`
	Tools           []ResponseToolDef `json:"tools,omitempty"`
	N               *int              `json:"n,omitempty"`
	ThinkingEffort  string            `json:"thinking_effort,omitempty"`
}

type ResponseToolDef struct {
	Type string `json:"type"`
}

type ResponseObject struct {
	ID        string               `json:"id"`
	Object    string               `json:"object"`
	CreatedAt int64                `json:"created_at"`
	Model     string               `json:"model"`
	Status    string               `json:"status"`
	Output    []ResponseOutputItem `json:"output"`
	Images    []MixedModeImage     `json:"images,omitempty"`
}

type ResponseOutputItem struct {
	ID      string                  `json:"id,omitempty"`
	Type    string                  `json:"type"`
	Status  string                  `json:"status,omitempty"`
	Role    string                  `json:"role,omitempty"`
	Content []ResponseOutputContent `json:"content,omitempty"`
	Result  []ResponseOutputImage   `json:"result,omitempty"`
}

type ResponseOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ResponseOutputImage struct {
	Type        string `json:"type"`
	URL         string `json:"url"`
	FileID      string `json:"file_id,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	IsPreview   bool   `json:"is_preview,omitempty"`
}
