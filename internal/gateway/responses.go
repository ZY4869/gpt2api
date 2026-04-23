package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
)

// Responses 是 POST /v1/responses 的首版 mixed-mode 入口。
func (h *Handler) Responses(c *gin.Context) {
	startAt := time.Now()
	ak, ok := apikey.FromCtx(c)
	if !ok {
		openAIError(c, http.StatusUnauthorized, "missing_api_key", "缺少 API Key")
		return
	}

	var req ResponseCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		openAIError(c, http.StatusBadRequest, "invalid_request_error", "请求参数错误:"+err.Error())
		return
	}
	refID := uuid.NewString()
	rec := &usage.Log{
		UserID:    ak.UserID,
		KeyID:     ak.ID,
		RequestID: refID,
		Type:      usage.TypeImage,
		IP:        c.ClientIP(),
		UA:        c.Request.UserAgent(),
	}
	defer func() {
		rec.DurationMs = int(time.Since(startAt).Milliseconds())
		if rec.Status == "" {
			rec.Status = usage.StatusFailed
		}
		h.writeUsage(rec)
	}()

	triggered := req.ImageGeneration || hasImageGenerationTool(req.Tools)
	if req.N != nil && !triggered {
		rec.ErrorCode = "invalid_request_error"
		openAIError(c, http.StatusBadRequest, "invalid_request_error",
			"n 仅可用于 image_generation=true 或 tools:[{type:\"image_generation\"}] 请求")
		return
	}
	if strings.TrimSpace(req.ThinkingEffort) != "" && !triggered {
		rec.ErrorCode = "invalid_request_error"
		openAIError(c, http.StatusBadRequest, "invalid_request_error",
			"thinking_effort 仅可用于 image_generation=true 或 tools:[{type:\"image_generation\"}] 请求")
		return
	}

	if !triggered {
		rec.ErrorCode = "invalid_request_error"
		openAIError(c, http.StatusBadRequest, "invalid_request_error",
			"/v1/responses 首版仅支持 image_generation=true 或 tools:[{type:\"image_generation\"}]")
		return
	}

	messages, err := responseInputToMessages(req.Input, req.Instructions)
	if err != nil {
		rec.ErrorCode = "invalid_request_error"
		openAIError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if req.Stream {
		h.streamMixedModeResponses(c, rec, ak, &req, messages)
		return
	}

	res, apiErr := h.callMixedModeChatImage(c, rec, ak, req.Model, mixedModeRequestInput{
		Messages:       messages,
		RequestedN:     req.N,
		WaitForResult:  req.WaitForResult,
		ThinkingEffort: req.ThinkingEffort,
	})
	if apiErr != nil {
		rec.Status = usage.StatusFailed
		rec.ErrorCode = apiErr.Code
		openAIError(c, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}

	responseID := "resp_" + newUUIDFunc()
	c.JSON(http.StatusOK, buildMixedModeResponseObject(responseID, req.Model, res))
}

func hasImageGenerationTool(tools []ResponseToolDef) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool.Type) == "image_generation" {
			return true
		}
	}
	return false
}

func responseInputToMessages(raw json.RawMessage, instructions string) ([]chatgpt.ChatMessage, error) {
	out := make([]chatgpt.ChatMessage, 0, 4)
	if strings.TrimSpace(instructions) != "" {
		out = append(out, chatgpt.ChatMessage{Role: "system", Content: instructions})
	}

	payload := strings.TrimSpace(string(raw))
	if payload == "" {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request_error",
			Message: "responses.input 不能为空",
		}
	}

	switch payload[0] {
	case '"':
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, &mixedModeAPIError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_request_error",
				Message: "responses.input 文本不能为空",
			}
		}
		out = append(out, chatgpt.ChatMessage{Role: "user", Content: text})
		return out, nil
	case '{':
		var msg map[string]interface{}
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, err
		}
		role := strings.TrimSpace(asString(msg["role"]))
		if role == "" {
			role = "user"
		}
		content := extractResponseContent(msg["content"])
		if strings.TrimSpace(content) == "" {
			return nil, &mixedModeAPIError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_request_error",
				Message: "responses.input.content 不能为空",
			}
		}
		out = append(out, chatgpt.ChatMessage{Role: role, Content: content})
		return out, nil
	case '[':
		var items []map[string]interface{}
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		for _, item := range items {
			role := strings.TrimSpace(asString(item["role"]))
			if role == "" {
				role = "user"
			}
			content := extractResponseContent(item["content"])
			if strings.TrimSpace(content) == "" {
				continue
			}
			out = append(out, chatgpt.ChatMessage{Role: role, Content: content})
		}
		if len(out) == 0 {
			return nil, &mixedModeAPIError{
				Status:  http.StatusBadRequest,
				Code:    "invalid_request_error",
				Message: "responses.input 里至少需要一条可解析的文本消息",
			}
		}
		return out, nil
	default:
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request_error",
			Message: "responses.input 仅支持字符串或消息数组",
		}
	}
}

func extractResponseContent(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case []interface{}:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			switch p := item.(type) {
			case string:
				if strings.TrimSpace(p) != "" {
					parts = append(parts, p)
				}
			case map[string]interface{}:
				if text := strings.TrimSpace(asString(p["text"])); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case map[string]interface{}:
		if text := strings.TrimSpace(asString(x["text"])); text != "" {
			return text
		}
	}
	return ""
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}
