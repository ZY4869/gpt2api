package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/432539/gpt2api/internal/apikey"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/scheduler"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
	"github.com/432539/gpt2api/pkg/logger"
)

type mixedModeExecResult struct {
	Status         string
	TaskID         string
	AccountID      uint64
	ConversationID string
	FileRefs       []string
	SignedURLs     []string
	ContentTypes   []string
	Images         []MixedModeImage
	IsPreview      bool
	AssistantText  string
	ReasoningText  string
	ImageTask      *MixedModeImageTask
}

type mixedModeAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *mixedModeAPIError) Error() string { return e.Message }

func (h *Handler) chatImageEnabled() bool {
	return h.Settings != nil && h.Settings.GatewayChatImageMixedEnabled()
}

func (h *Handler) mixedModeMaxN() int {
	if h.Settings == nil {
		return defaultMixedModeMaxN
	}
	n := h.Settings.GatewayChatImageMaxN()
	if n <= 0 {
		return defaultMixedModeMaxN
	}
	if n > defaultMixedModeMaxN {
		return defaultMixedModeMaxN
	}
	return n
}

func (h *Handler) mixedModeImageStrategy(chatModel *modelpkg.Model) string {
	if !isThinkingModel(chatModel) {
		return chatgpt.ImageStrategyPictureV2Thinking
	}
	if h.Settings == nil {
		return chatgpt.ImageStrategyPictureV2Thinking
	}
	return chatgpt.NormalizeImageStrategy(h.Settings.GatewayChatImageThinkingStrategy())
}

func (h *Handler) mixedModeWaitForResult(v *bool) bool {
	if v != nil {
		return *v
	}
	if h.Settings != nil {
		return h.Settings.GatewayChatImageDefaultWaitForResult()
	}
	return true
}

func (h *Handler) resolveMixedModeRequestInput(input mixedModeRequestInput) mixedModeRequestInput {
	waitForResult := h.mixedModeWaitForResult(input.WaitForResult)
	input.WaitForResult = &waitForResult
	return input
}

func (h *Handler) handleChatImageGeneration(c *gin.Context, rec *usage.Log, ak *apikey.APIKey, req *ChatCompletionsRequest) {
	if req.Stream {
		h.streamMixedModeChatCompletions(c, rec, ak, req)
		return
	}

	res, apiErr := h.callMixedModeChatImage(c, rec, ak, req.Model, mixedModeRequestInput{
		Messages:       req.Messages,
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

	c.JSON(http.StatusOK, ChatCompletionResponse{
		ID:      "chatcmpl-" + newUUIDFunc(),
		Object:  "chat.completion",
		Created: nowFunc().Unix(),
		Model:   req.Model,
		Choices: []ChatCompletionChoice{{
			Index: 0,
			Message: chatgpt.ChatMessage{
				Role:    "assistant",
				Content: res.responseText(),
			},
			Reasoning:    strings.TrimSpace(res.ReasoningText),
			FinishReason: "stop",
		}},
		Usage:     ChatCompletionUsage{},
		Images:    res.Images,
		ImageTask: res.ImageTask,
	})
}

func (h *Handler) executeMixedModeChatImage(
	c *gin.Context,
	rec *usage.Log,
	ak *apikey.APIKey,
	requestedModel string,
	input mixedModeRequestInput,
) (*mixedModeExecResult, *mixedModeAPIError) {
	return h.executeMixedModeChatImageWithSink(c, rec, ak, requestedModel, input, nil)
}

func (h *Handler) resolveMixedModeBillingModel(ctx context.Context) (*modelpkg.Model, error) {
	if m, err := h.Models.BySlug(ctx, "gpt-image-2"); err == nil && m != nil && m.Enabled && m.Type == modelpkg.TypeImage {
		return m, nil
	}
	list, err := h.Models.ListEnabled(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range list {
		if m != nil && m.Enabled && m.Type == modelpkg.TypeImage {
			return m, nil
		}
	}
	return nil, modelpkg.ErrNotFound
}

func (h *Handler) resolveGroupRatio(ctx context.Context, ak *apikey.APIKey) (float64, int) {
	ratio := 1.0
	rpmCap := ak.RPM
	if h.Groups != nil {
		if g, err := h.Groups.OfUser(ctx, ak.UserID); err == nil && g != nil {
			ratio = g.Ratio
			if rpmCap == 0 {
				rpmCap = g.RPMLimit
			}
		}
	}
	return ratio, rpmCap
}

func (h *Handler) mixedModeUpstreamError(
	_ context.Context,
	lease *scheduler.Lease,
	err error,
) *mixedModeAPIError {
	var ue *chatgpt.UpstreamError
	if errors.As(err, &ue) {
		switch {
		case ue.IsRateLimited():
			h.markRateLimitedAccount(context.Background(), lease.Account.ID)
		case ue.IsUnauthorized():
			h.markDeadAccount(context.Background(), lease.Account.ID)
		}
		logger.L().Error("chat mixed image upstream error",
			zap.Uint64("account_id", lease.Account.ID),
			zap.Int("status", ue.Status),
			zap.String("body", truncate(ue.Body, 1500)))
		return &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_error",
			Message: fmt.Sprintf("上游返回错误(HTTP %d):%s", ue.Status, truncate(ue.Body, 200)),
		}
	}
	return &mixedModeAPIError{
		Status:  http.StatusBadGateway,
		Code:    "upstream_error",
		Message: "上游请求失败:" + err.Error(),
	}
}
