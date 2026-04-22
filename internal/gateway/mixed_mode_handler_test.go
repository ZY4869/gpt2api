package gateway

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/usage"
)

func TestChatCompletionsMixedMode_Golden(t *testing.T) {
	fixedAt := time.Unix(1710806400, 0).UTC()
	ak := &apikey.APIKey{ID: 7, UserID: 9, Enabled: true}

	cases := []struct {
		name       string
		body       string
		golden     string
		statusCode int
		settings   fakeSettings
		exec       mixedModeExecFunc
		wantCode   string
		wantStatus string
		wantType   string
	}{
		{
			name:       "success",
			body:       `{"model":"gpt-5","image_generation":true,"stream":false,"messages":[{"role":"user","content":"画一只橘猫"}]}`,
			golden:     "chat_mixed_success.json",
			statusCode: http.StatusOK,
			settings:   fakeSettings{mixedEnabled: true},
			exec: func(_ *gin.Context, rec *usage.Log, _ *apikey.APIKey, _ string, input mixedModeRequestInput) (*mixedModeExecResult, *mixedModeAPIError) {
				rec.Type = usage.TypeImage
				rec.Status = usage.StatusSuccess
				rec.AccountID = 88
				rec.ImageCount = 1
				rec.CreditCost = 440
				if input.RequestedN != nil {
					t.Fatalf("expected nil requested_n, got %d", *input.RequestedN)
				}
				return &mixedModeExecResult{
					Images: []MixedModeImage{{
						URL:         "/p/img/task_mixed/0?exp=1710892800000&sig=abc123mixed",
						FileID:      "file_mixed_1",
						ContentType: "image/png",
						TaskID:      "task_mixed",
					}},
				}, nil
			},
			wantStatus: usage.StatusSuccess,
			wantType:   usage.TypeImage,
		},
		{
			name:       "stream_unsupported",
			body:       `{"model":"gpt-5","image_generation":true,"stream":true,"messages":[{"role":"user","content":"画一只橘猫"}]}`,
			golden:     "chat_mixed_stream_unsupported.json",
			statusCode: http.StatusBadRequest,
			settings:   fakeSettings{mixedEnabled: true},
			wantCode:   "image_generation_stream_unsupported",
			wantStatus: usage.StatusFailed,
			wantType:   usage.TypeChat,
		},
		{
			name:       "feature_disabled",
			body:       `{"model":"gpt-5","image_generation":true,"stream":false,"messages":[{"role":"user","content":"画一只橘猫"}]}`,
			golden:     "chat_mixed_feature_disabled.json",
			statusCode: http.StatusForbidden,
			settings:   fakeSettings{mixedEnabled: false},
			wantCode:   "feature_disabled",
			wantStatus: usage.StatusFailed,
			wantType:   usage.TypeChat,
		},
		{
			name:       "upstream_not_returned",
			body:       `{"model":"gpt-5","image_generation":true,"stream":false,"messages":[{"role":"user","content":"画一只橘猫"}]}`,
			golden:     "chat_mixed_upstream_not_returned.json",
			statusCode: http.StatusBadGateway,
			settings:   fakeSettings{mixedEnabled: true},
			exec: func(_ *gin.Context, rec *usage.Log, _ *apikey.APIKey, _ string, _ mixedModeRequestInput) (*mixedModeExecResult, *mixedModeAPIError) {
				rec.Type = usage.TypeImage
				return nil, &mixedModeAPIError{
					Status:  http.StatusBadGateway,
					Code:    "upstream_image_not_returned",
					Message: "上游本轮对话未返回图片结果,没有自动降级到旧图片接口",
				}
			},
			wantCode:   "upstream_image_not_returned",
			wantStatus: usage.StatusFailed,
			wantType:   usage.TypeImage,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFrozenResponseMeta(t, fixedAt, "mixed-chat-1")
			usageSink := &fakeUsageStore{}
			h := &Handler{
				Usage:         usageSink,
				Settings:      tc.settings,
				mixedModeExec: tc.exec,
			}
			if tc.name == "feature_disabled" {
				h.Images = &ImagesHandler{
					Handler:          h,
					DAO:              &fakeImageTaskStore{},
					ImageAccResolver: stubImageAccountResolver{},
				}
			}
			c, w := newJSONContext(t, "/v1/chat/completions", tc.body, ak)
			h.ChatCompletions(c)

			if w.Code != tc.statusCode {
				t.Fatalf("status = %d, want %d", w.Code, tc.statusCode)
			}
			assertJSONGolden(t, w.Body.Bytes(), tc.golden)
			if len(usageSink.rows) != 1 {
				t.Fatalf("usage rows = %d, want 1", len(usageSink.rows))
			}
			if usageSink.rows[0].Status != tc.wantStatus {
				t.Fatalf("usage status = %s, want %s", usageSink.rows[0].Status, tc.wantStatus)
			}
			if usageSink.rows[0].Type != tc.wantType {
				t.Fatalf("usage type = %s, want %s", usageSink.rows[0].Type, tc.wantType)
			}
			if tc.wantCode != "" && usageSink.rows[0].ErrorCode != tc.wantCode {
				t.Fatalf("usage code = %s, want %s", usageSink.rows[0].ErrorCode, tc.wantCode)
			}
		})
	}
}

func TestResponsesMixedMode_Golden(t *testing.T) {
	fixedAt := time.Unix(1710806400, 0).UTC()
	ak := &apikey.APIKey{ID: 7, UserID: 9, Enabled: true}

	cases := []struct {
		name       string
		body       string
		golden     string
		statusCode int
		exec       mixedModeExecFunc
		wantCode   string
		wantStatus string
	}{
		{
			name:       "success",
			body:       `{"model":"gpt-5","input":"画一张极简主义太空旅行海报","tools":[{"type":"image_generation"}],"stream":false}`,
			golden:     "responses_mixed_success.json",
			statusCode: http.StatusOK,
			exec: func(_ *gin.Context, rec *usage.Log, _ *apikey.APIKey, _ string, input mixedModeRequestInput) (*mixedModeExecResult, *mixedModeAPIError) {
				rec.Status = usage.StatusSuccess
				rec.AccountID = 66
				rec.ImageCount = 1
				rec.CreditCost = 520
				if input.RequestedN != nil {
					t.Fatalf("expected nil requested_n, got %d", *input.RequestedN)
				}
				return &mixedModeExecResult{
					Images: []MixedModeImage{{
						URL:         "/p/img/task_response/0?exp=1710892800000&sig=resp123mixed",
						FileID:      "file_response_1",
						ContentType: "image/png",
						TaskID:      "task_response",
					}},
				}, nil
			},
			wantStatus: usage.StatusSuccess,
		},
		{
			name:       "invalid_trigger",
			body:       `{"model":"gpt-5","input":"你好","stream":false}`,
			golden:     "responses_mixed_invalid_trigger.json",
			statusCode: http.StatusBadRequest,
			wantCode:   "invalid_request_error",
			wantStatus: usage.StatusFailed,
		},
		{
			name:       "stream_unsupported",
			body:       `{"model":"gpt-5","input":"你好","image_generation":true,"stream":true}`,
			golden:     "responses_mixed_stream_unsupported.json",
			statusCode: http.StatusBadRequest,
			wantCode:   "image_generation_stream_unsupported",
			wantStatus: usage.StatusFailed,
		},
		{
			name:       "upstream_not_returned",
			body:       `{"model":"gpt-5","input":"画一张极简主义太空旅行海报","tools":[{"type":"image_generation"}],"stream":false}`,
			golden:     "responses_mixed_upstream_not_returned.json",
			statusCode: http.StatusBadGateway,
			exec: func(_ *gin.Context, rec *usage.Log, _ *apikey.APIKey, _ string, _ mixedModeRequestInput) (*mixedModeExecResult, *mixedModeAPIError) {
				rec.Type = usage.TypeImage
				return nil, &mixedModeAPIError{
					Status:  http.StatusBadGateway,
					Code:    "upstream_image_not_returned",
					Message: "上游本轮对话未返回图片结果,没有自动降级到旧图片接口",
				}
			},
			wantCode:   "upstream_image_not_returned",
			wantStatus: usage.StatusFailed,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withFrozenResponseMeta(t, fixedAt, "mixed-response-1", "mixed-image-call-1")
			usageSink := &fakeUsageStore{}
			h := &Handler{
				Usage:         usageSink,
				Settings:      fakeSettings{mixedEnabled: true},
				mixedModeExec: tc.exec,
			}
			c, w := newJSONContext(t, "/v1/responses", tc.body, ak)
			h.Responses(c)

			if w.Code != tc.statusCode {
				t.Fatalf("status = %d, want %d", w.Code, tc.statusCode)
			}
			assertJSONGolden(t, w.Body.Bytes(), tc.golden)
			if len(usageSink.rows) != 1 {
				t.Fatalf("usage rows = %d, want 1", len(usageSink.rows))
			}
			if usageSink.rows[0].Status != tc.wantStatus {
				t.Fatalf("usage status = %s, want %s", usageSink.rows[0].Status, tc.wantStatus)
			}
			if tc.wantCode != "" && usageSink.rows[0].ErrorCode != tc.wantCode {
				t.Fatalf("usage code = %s, want %s", usageSink.rows[0].ErrorCode, tc.wantCode)
			}
		})
	}
}

func TestMixedModeRejectsNWithoutImageGeneration(t *testing.T) {
	ak := &apikey.APIKey{ID: 7, UserID: 9, Enabled: true}

	t.Run("chat_completions", func(t *testing.T) {
		usageSink := &fakeUsageStore{}
		h := &Handler{Usage: usageSink, Settings: fakeSettings{mixedEnabled: true}}
		c, w := newJSONContext(t, "/v1/chat/completions", `{"model":"gpt-5","n":2,"messages":[{"role":"user","content":"你好"}]}`, ak)
		h.ChatCompletions(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})

	t.Run("responses", func(t *testing.T) {
		usageSink := &fakeUsageStore{}
		h := &Handler{Usage: usageSink, Settings: fakeSettings{mixedEnabled: true}}
		c, w := newJSONContext(t, "/v1/responses", `{"model":"gpt-5","n":2,"input":"你好","stream":false}`, ak)
		h.Responses(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

func TestMixedModePassesNAndThinkingEffortToExecutor(t *testing.T) {
	ak := &apikey.APIKey{ID: 7, UserID: 9, Enabled: true}

	t.Run("chat_completions", func(t *testing.T) {
		usageSink := &fakeUsageStore{}
		h := &Handler{
			Usage:    usageSink,
			Settings: fakeSettings{mixedEnabled: true},
			mixedModeExec: func(_ *gin.Context, rec *usage.Log, _ *apikey.APIKey, _ string, input mixedModeRequestInput) (*mixedModeExecResult, *mixedModeAPIError) {
				rec.Type = usage.TypeImage
				rec.Status = usage.StatusSuccess
				if input.RequestedN == nil || *input.RequestedN != 3 {
					t.Fatalf("requested_n = %#v, want 3", input.RequestedN)
				}
				if input.ThinkingEffort != "high" {
					t.Fatalf("thinking_effort = %q, want high", input.ThinkingEffort)
				}
				return &mixedModeExecResult{Images: []MixedModeImage{{TaskID: "task_chat", FileID: "file_chat", URL: "/p/img/task_chat/0", ContentType: "image/png"}}}, nil
			},
		}
		c, w := newJSONContext(t, "/v1/chat/completions", `{"model":"gpt-5-thinking","image_generation":true,"n":3,"thinking_effort":"high","stream":false,"messages":[{"role":"user","content":"生成3张连续故事图"}]}`, ak)
		h.ChatCompletions(c)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})

	t.Run("responses", func(t *testing.T) {
		usageSink := &fakeUsageStore{}
		h := &Handler{
			Usage:    usageSink,
			Settings: fakeSettings{mixedEnabled: true},
			mixedModeExec: func(_ *gin.Context, rec *usage.Log, _ *apikey.APIKey, _ string, input mixedModeRequestInput) (*mixedModeExecResult, *mixedModeAPIError) {
				rec.Type = usage.TypeImage
				rec.Status = usage.StatusSuccess
				if input.RequestedN == nil || *input.RequestedN != 3 {
					t.Fatalf("requested_n = %#v, want 3", input.RequestedN)
				}
				if input.ThinkingEffort != "high" {
					t.Fatalf("thinking_effort = %q, want high", input.ThinkingEffort)
				}
				return &mixedModeExecResult{Images: []MixedModeImage{{TaskID: "task_resp", FileID: "file_resp", URL: "/p/img/task_resp/0", ContentType: "image/png"}}}, nil
			},
		}
		c, w := newJSONContext(t, "/v1/responses", `{"model":"gpt-5-thinking","input":"生成3张连续故事图","tools":[{"type":"image_generation"}],"n":3,"thinking_effort":"high","stream":false}`, ak)
		h.Responses(c)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
	})
}

func TestMixedModeRejectsThinkingEffortWithoutImageGeneration(t *testing.T) {
	ak := &apikey.APIKey{ID: 7, UserID: 9, Enabled: true}

	t.Run("chat_completions", func(t *testing.T) {
		usageSink := &fakeUsageStore{}
		h := &Handler{Usage: usageSink, Settings: fakeSettings{mixedEnabled: true}}
		c, w := newJSONContext(t, "/v1/chat/completions", `{"model":"gpt-5","thinking_effort":"high","messages":[{"role":"user","content":"你好"}]}`, ak)
		h.ChatCompletions(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})

	t.Run("responses", func(t *testing.T) {
		usageSink := &fakeUsageStore{}
		h := &Handler{Usage: usageSink, Settings: fakeSettings{mixedEnabled: true}}
		c, w := newJSONContext(t, "/v1/responses", `{"model":"gpt-5","thinking_effort":"high","input":"你好","stream":false}`, ak)
		h.Responses(c)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", w.Code)
		}
	})
}

func TestMixedModeReturnsReasoningContent(t *testing.T) {
	ak := &apikey.APIKey{ID: 7, UserID: 9, Enabled: true}

	t.Run("chat_completions", func(t *testing.T) {
		withFrozenResponseMeta(t, time.Unix(1710806400, 0).UTC(), "mixed-chat-reasoning")
		h := &Handler{
			Usage:    &fakeUsageStore{},
			Settings: fakeSettings{mixedEnabled: true},
			mixedModeExec: func(_ *gin.Context, rec *usage.Log, _ *apikey.APIKey, _ string, _ mixedModeRequestInput) (*mixedModeExecResult, *mixedModeAPIError) {
				rec.Type = usage.TypeImage
				rec.Status = usage.StatusSuccess
				return &mixedModeExecResult{
					ReasoningText: "先规划两张故事书分镜，再保证角色动作连贯。",
					AssistantText: "我会生成两张连续故事图。",
					Images: []MixedModeImage{{
						URL:         "/p/img/task_reasoning/0?exp=1710892800000&sig=chatreason",
						FileID:      "file_reasoning_1",
						ContentType: "image/png",
						TaskID:      "task_reasoning",
					}},
				}, nil
			},
		}
		c, w := newJSONContext(t, "/v1/chat/completions", `{"model":"gpt-5-thinking","image_generation":true,"stream":false,"messages":[{"role":"user","content":"生成两张连续故事图"}]}`, ak)
		h.ChatCompletions(c)
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
		wantContent := "先规划两张故事书分镜，再保证角色动作连贯。\n\n我会生成两张连续故事图。"
		if resp.Choices[0].Message.Content != wantContent {
			t.Fatalf("message.content = %q, want %q", resp.Choices[0].Message.Content, wantContent)
		}
		if resp.Choices[0].Reasoning != "先规划两张故事书分镜，再保证角色动作连贯。" {
			t.Fatalf("reasoning = %q", resp.Choices[0].Reasoning)
		}
	})

	t.Run("responses", func(t *testing.T) {
		withFrozenResponseMeta(t, time.Unix(1710806400, 0).UTC(), "mixed-response-reasoning", "mixed-response-message", "mixed-response-image")
		h := &Handler{
			Usage:    &fakeUsageStore{},
			Settings: fakeSettings{mixedEnabled: true},
			mixedModeExec: func(_ *gin.Context, rec *usage.Log, _ *apikey.APIKey, _ string, _ mixedModeRequestInput) (*mixedModeExecResult, *mixedModeAPIError) {
				rec.Type = usage.TypeImage
				rec.Status = usage.StatusSuccess
				return &mixedModeExecResult{
					ReasoningText: "先安排起床，再收束到山顶作画。",
					AssistantText: "我会保持同一画风和透明背景。",
					Images: []MixedModeImage{{
						URL:         "/p/img/task_resp_reasoning/0?exp=1710892800000&sig=respreason",
						FileID:      "file_resp_reasoning_1",
						ContentType: "image/png",
						TaskID:      "task_resp_reasoning",
					}},
				}, nil
			},
		}
		c, w := newJSONContext(t, "/v1/responses", `{"model":"gpt-5-thinking","input":"生成两张连续故事图","tools":[{"type":"image_generation"}],"stream":false}`, ak)
		h.Responses(c)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		var resp ResponseObject
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Output) != 2 {
			t.Fatalf("output len = %d, want 2", len(resp.Output))
		}
		if resp.Output[0].Type != "message" || resp.Output[0].Role != "assistant" {
			t.Fatalf("first output = %#v, want assistant message", resp.Output[0])
		}
		if len(resp.Output[0].Content) != 1 || resp.Output[0].Content[0].Type != "output_text" {
			t.Fatalf("message content = %#v", resp.Output[0].Content)
		}
		wantText := "先安排起床，再收束到山顶作画。\n\n我会保持同一画风和透明背景。"
		if resp.Output[0].Content[0].Text != wantText {
			t.Fatalf("message text = %q, want %q", resp.Output[0].Content[0].Text, wantText)
		}
		if resp.Output[1].Type != "image_generation_call" {
			t.Fatalf("second output type = %q, want image_generation_call", resp.Output[1].Type)
		}
	})
}
