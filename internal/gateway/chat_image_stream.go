package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
)

type mixedModeChatChunkSink struct {
	w     io.Writer
	f     http.Flusher
	id    string
	model string
}

func (s *mixedModeChatChunkSink) OnReasoningDelta(text string) {
	writeChunkWithImages(s.w, s.f, s.id, s.model, DeltaMsg{Reasoning: text}, nil, nil, nil)
}

func (s *mixedModeChatChunkSink) OnAssistantDelta(text string) {
	writeChunkWithImages(s.w, s.f, s.id, s.model, DeltaMsg{Content: text}, nil, nil, nil)
}

type mixedModeResponsesSink struct {
	w io.Writer
	f http.Flusher
}

func (s *mixedModeResponsesSink) OnReasoningDelta(text string) {
	writeSSEJSONEvent(s.w, s.f, "response.reasoning.delta", gin.H{
		"type":  "response.reasoning.delta",
		"delta": text,
	})
}

func (s *mixedModeResponsesSink) OnAssistantDelta(text string) {
	writeSSEJSONEvent(s.w, s.f, "response.output_text.delta", gin.H{
		"type":  "response.output_text.delta",
		"delta": text,
	})
}

func (h *Handler) streamMixedModeChatCompletions(
	c *gin.Context,
	rec *usage.Log,
	ak *apikey.APIKey,
	req *ChatCompletionsRequest,
) {
	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	id := "chatcmpl-" + newUUIDFunc()
	writeChunkWithImages(w, flusher, id, req.Model, DeltaMsg{Role: "assistant"}, nil, nil, nil)

	res, apiErr := h.executeMixedModeChatImageWithSink(c, rec, ak, req.Model, mixedModeRequestInput{
		Messages:       req.Messages,
		RequestedN:     req.N,
		ThinkingEffort: req.ThinkingEffort,
	}, &mixedModeChatChunkSink{
		w:     w,
		f:     flusher,
		id:    id,
		model: req.Model,
	})
	if apiErr != nil {
		rec.Status = usage.StatusFailed
		rec.ErrorCode = apiErr.Code
		writeOpenAIErrorEvent(w, flusher, apiErr.Code, apiErr.Message)
		return
	}

	stop := "stop"
	writeChunkWithImages(w, flusher, id, req.Model, DeltaMsg{}, &stop, res.Images, res.ImageTask)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func (h *Handler) streamMixedModeResponses(
	c *gin.Context,
	rec *usage.Log,
	ak *apikey.APIKey,
	req *ResponseCreateRequest,
	messages []chatgpt.ChatMessage,
) {
	w := c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	responseID := "resp_" + newUUIDFunc()
	writeSSEJSONEvent(w, flusher, "response.created", gin.H{
		"id":         responseID,
		"object":     "response",
		"created_at": nowFunc().Unix(),
		"model":      req.Model,
		"status":     "in_progress",
	})

	res, apiErr := h.executeMixedModeChatImageWithSink(c, rec, ak, req.Model, mixedModeRequestInput{
		Messages:       messages,
		RequestedN:     req.N,
		ThinkingEffort: req.ThinkingEffort,
	}, &mixedModeResponsesSink{w: w, f: flusher})
	if apiErr != nil {
		rec.Status = usage.StatusFailed
		rec.ErrorCode = apiErr.Code
		writeSSEJSONEvent(w, flusher, "response.failed", gin.H{
			"type": "response.failed",
			"error": gin.H{
				"message": apiErr.Message,
				"type":    "invalid_request_error",
				"code":    apiErr.Code,
			},
		})
		return
	}

	outputImages := buildResponseOutputImages(res.Images)
	if res.Status == mixedModeExecStatusInProgress {
		payload := gin.H{
			"type":   "response.image_generation_call.in_progress",
			"status": "in_progress",
			"result": outputImages,
		}
		if res.ImageTask != nil {
			payload["image_task"] = res.ImageTask
		}
		writeSSEJSONEvent(w, flusher, "response.image_generation_call.in_progress", payload)
		writeSSEJSONEvent(w, flusher, "response.in_progress", buildMixedModeResponseObject(responseID, req.Model, res))
		return
	}
	payload := gin.H{
		"type":   "response.image_generation_call.completed",
		"status": "completed",
		"result": outputImages,
	}
	if res.ImageTask != nil {
		payload["image_task"] = res.ImageTask
	}
	writeSSEJSONEvent(w, flusher, "response.image_generation_call.completed", payload)
	writeSSEJSONEvent(w, flusher, "response.completed", buildMixedModeResponseObject(responseID, req.Model, res))
}

func buildResponseOutputImages(images []MixedModeImage) []ResponseOutputImage {
	out := make([]ResponseOutputImage, 0, len(images))
	for _, img := range images {
		out = append(out, ResponseOutputImage{
			Type:        "output_image",
			URL:         img.URL,
			FileID:      img.FileID,
			ContentType: img.ContentType,
			TaskID:      img.TaskID,
			IsPreview:   img.IsPreview,
		})
	}
	return out
}

func buildMixedModeResponseObject(responseID, model string, res *mixedModeExecResult) ResponseObject {
	outputItems := make([]ResponseOutputItem, 0, 2)
	status := "completed"
	imageStatus := "completed"
	if res != nil && res.Status == mixedModeExecStatusInProgress {
		status = "in_progress"
		imageStatus = "in_progress"
	}
	if text := res.responseText(); text != "" {
		outputItems = append(outputItems, ResponseOutputItem{
			ID:     "msg_" + newUUIDFunc(),
			Type:   "message",
			Role:   "assistant",
			Status: "completed",
			Content: []ResponseOutputContent{{
				Type: "output_text",
				Text: text,
			}},
		})
	}
	outputItems = append(outputItems, ResponseOutputItem{
		ID:     "igc_" + newUUIDFunc(),
		Type:   "image_generation_call",
		Status: imageStatus,
		Result: buildResponseOutputImages(res.Images),
	})
	return ResponseObject{
		ID:        responseID,
		Object:    "response",
		CreatedAt: nowFunc().Unix(),
		Model:     model,
		Status:    status,
		Output:    outputItems,
		Images:    res.Images,
		ImageTask: res.ImageTask,
	}
}

func writeChunkWithImages(
	w io.Writer,
	f http.Flusher,
	id, model string,
	delta DeltaMsg,
	finish *string,
	images []MixedModeImage,
	imageTask *MixedModeImageTask,
) {
	chunk := ChatCompletionChunk{
		ID:        id,
		Object:    "chat.completion.chunk",
		Created:   nowFunc().Unix(),
		Model:     model,
		Choices:   []ChatCompletionChunkChoice{{Index: 0, Delta: delta, FinishReason: finish}},
		Images:    images,
		ImageTask: imageTask,
	}
	b, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", b)
	if f != nil {
		f.Flush()
	}
}

func writeOpenAIErrorEvent(w io.Writer, f http.Flusher, code, message string) {
	writeSSEJSONEvent(w, f, "error", gin.H{
		"error": gin.H{
			"message": message,
			"type":    "invalid_request_error",
			"code":    code,
		},
	})
}

func writeSSEJSONEvent(w io.Writer, f http.Flusher, event string, payload interface{}) {
	body, _ := json.Marshal(payload)
	if strings.TrimSpace(event) != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", body)
	if f != nil {
		f.Flush()
	}
}
