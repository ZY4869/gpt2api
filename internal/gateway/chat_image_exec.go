package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/billing"
	"github.com/432539/gpt2api/internal/image"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/scheduler"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
	"github.com/432539/gpt2api/pkg/logger"
)

func (h *Handler) executeMixedModeChatImageWithSink(
	c *gin.Context,
	rec *usage.Log,
	ak *apikey.APIKey,
	requestedModel string,
	input mixedModeRequestInput,
	sink mixedModeStreamSink,
) (*mixedModeExecResult, *mixedModeAPIError) {
	if !h.chatImageEnabled() {
		return nil, &mixedModeAPIError{
			Status:  http.StatusForbidden,
			Code:    "feature_disabled",
			Message: "对话框生图功能未开启,请联系管理员启用 gateway.chat_image_mixed_enabled",
		}
	}
	if h.Images == nil || h.Images.DAO == nil || h.Images.ImageAccResolver == nil {
		return nil, &mixedModeAPIError{
			Status:  http.StatusNotImplemented,
			Code:    "image_not_wired",
			Message: "图片生成能力未完整接入,请联系管理员",
		}
	}

	if !ak.ModelAllowed(requestedModel) {
		return nil, &mixedModeAPIError{
			Status:  http.StatusForbidden,
			Code:    "model_not_allowed",
			Message: fmt.Sprintf("当前 API Key 无权调用模型 %q", requestedModel),
		}
	}
	chatModel, err := h.Models.BySlug(c.Request.Context(), requestedModel)
	if err != nil || chatModel == nil || !chatModel.Enabled {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadRequest,
			Code:    "model_not_found",
			Message: modelNotFoundMessage(requestedModel),
		}
	}
	if chatModel.Type != modelpkg.TypeChat {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadRequest,
			Code:    "model_type_mismatch",
			Message: "image_generation=true 仅支持 chat/thinking 模型,不支持直接传图片模型",
		}
	}

	mixedReq, apiErr := prepareMixedModeRequest(chatModel, input, h.mixedModeMaxN())
	if apiErr != nil {
		return nil, apiErr
	}
	strategy := h.mixedModeImageStrategy(chatModel)

	imageModel, err := h.resolveMixedModeBillingModel(c.Request.Context())
	if err != nil {
		return nil, &mixedModeAPIError{
			Status:  http.StatusServiceUnavailable,
			Code:    "image_not_wired",
			Message: "未找到可用于 mixed-mode 计费和落库的图片模型配置",
		}
	}

	ratio, rpmCap := h.resolveGroupRatio(c.Request.Context(), ak)
	rec.Type = usage.TypeImage
	rec.ModelID = imageModel.ID

	if h.Limiter != nil {
		if ok, _, err := h.Limiter.AllowRPM(c.Request.Context(), ak.ID, rpmCap); err == nil && !ok {
			return nil, &mixedModeAPIError{
				Status:  http.StatusTooManyRequests,
				Code:    "rate_limit_rpm",
				Message: "触发每分钟请求数限制 (RPM),请稍后再试",
			}
		}
	}

	refID := rec.RequestID
	estimatedCost := billing.ComputeImageCost(imageModel, mixedReq.RequestedN, ratio)
	if estimatedCost > 0 {
		if err := h.Billing.PreDeduct(c.Request.Context(), ak.UserID, ak.ID, estimatedCost, refID, "chat image mixed prepay"); err != nil {
			if errors.Is(err, billing.ErrInsufficient) {
				return nil, &mixedModeAPIError{
					Status:  http.StatusPaymentRequired,
					Code:    "insufficient_balance",
					Message: "积分不足,请前往「账单与充值」充值后再试",
				}
			}
			return nil, &mixedModeAPIError{
				Status:  http.StatusInternalServerError,
				Code:    "billing_error",
				Message: "计费异常:" + err.Error(),
			}
		}
	}

	taskID := imageTaskIDFunc()
	taskCreated := false
	refunded := false
	refund := func(code, remark string) {
		if taskCreated && h.Images != nil && h.Images.DAO != nil {
			_ = h.Images.DAO.MarkFailed(context.Background(), taskID, code)
		}
		if refunded || estimatedCost == 0 {
			return
		}
		refunded = true
		_ = h.Billing.Refund(context.Background(), ak.UserID, ak.ID, estimatedCost, refID, remark)
	}

	if err := h.Images.DAO.Create(c.Request.Context(), &image.Task{
		TaskID:          taskID,
		UserID:          ak.UserID,
		KeyID:           ak.ID,
		ModelID:         imageModel.ID,
		Prompt:          mixedReq.Prompt,
		N:               mixedReq.RequestedN,
		Size:            "1024x1024",
		Status:          image.StatusDispatched,
		EstimatedCredit: estimatedCost,
	}); err != nil {
		refund("billing_error", "chat image mixed refund")
		return nil, &mixedModeAPIError{
			Status:  http.StatusInternalServerError,
			Code:    "internal_error",
			Message: "创建图片任务失败:" + err.Error(),
		}
	}
	taskCreated = true

	runCtx, cancel := context.WithTimeout(c.Request.Context(), h.mixedModeRunTimeout(chatModel))
	defer cancel()

	logger.L().Info("chat mixed image start",
		zap.String("request_id", rec.RequestID),
		zap.String("chat_model", requestedModel),
		zap.String("requested_model_slug", requestedModel),
		zap.String("resolved_upstream_model_slug", resolveRequestedUpstreamModel(chatModel)),
		zap.String("task_id", taskID),
		zap.Int("requested_n", mixedReq.RequestedN),
		zap.Int("actual_n", 0),
		zap.String("thinking_effort", mixedReq.ThinkingEffort),
		zap.String("strategy", strategy),
		zap.String("conversation_id", ""),
		zap.Bool("partial_success", false),
		zap.Bool("thinking_model", isThinkingModel(chatModel)),
		zap.Bool("require_paid", isThinkingModel(chatModel)),
	)

	var res *mixedModeExecResult
	if sink != nil {
		res, apiErr = h.callMixedModeConversationStream(runCtx, taskID, chatModel, mixedReq, sink)
	} else {
		res, apiErr = h.callMixedModeConversation(runCtx, taskID, chatModel, mixedReq)
	}
	if apiErr != nil {
		refund(apiErr.Code, "chat image mixed refund")
		return nil, apiErr
	}
	actualCost := billing.ComputeImageCost(imageModel, len(res.Images), ratio)
	if err := h.Billing.Settle(context.Background(), ak.UserID, ak.ID, estimatedCost, actualCost, refID, "chat image mixed settle"); err != nil {
		logger.L().Error("billing settle chat mixed image", zap.Error(err), zap.String("ref", refID))
	}
	h.touchKeyUsage(context.Background(), ak.ID, c.ClientIP(), actualCost)

	if err := h.Images.DAO.MarkSuccess(c.Request.Context(), taskID, res.ConversationID, res.FileRefs, res.SignedURLs, actualCost); err != nil {
		logger.L().Warn("chat mixed image mark success failed",
			zap.String("task_id", taskID), zap.Error(err))
	}
	if err := h.Images.DAO.UpdateCost(c.Request.Context(), taskID, actualCost); err != nil {
		logger.L().Warn("chat mixed image update cost failed",
			zap.String("task_id", taskID), zap.Error(err))
	}

	rec.Status = usage.StatusSuccess
	rec.AccountID = res.AccountID
	rec.ImageCount = len(res.Images)
	rec.CreditCost = actualCost
	partialSuccess := len(res.Images) < mixedReq.RequestedN
	if partialSuccess {
		logger.L().Warn("chat mixed image partial success",
			zap.String("request_id", rec.RequestID),
			zap.String("task_id", taskID),
			zap.Int("requested_n", mixedReq.RequestedN),
			zap.Int("actual_n", len(res.Images)),
			zap.String("thinking_effort", mixedReq.ThinkingEffort),
			zap.String("strategy", strategy),
			zap.String("conversation_id", res.ConversationID),
			zap.Bool("partial_success", true),
			zap.Bool("thinking_triggered", strings.TrimSpace(res.ReasoningText) != ""),
			zap.Int("reasoning_len", len(strings.TrimSpace(res.ReasoningText))),
		)
	}

	logger.L().Info("chat mixed image success",
		zap.String("request_id", rec.RequestID),
		zap.String("task_id", taskID),
		zap.String("requested_model_slug", requestedModel),
		zap.Uint64("account_id", res.AccountID),
		zap.String("conversation_id", res.ConversationID),
		zap.Int("requested_n", mixedReq.RequestedN),
		zap.Int("actual_n", len(res.Images)),
		zap.Int("images", len(res.Images)),
		zap.String("thinking_effort", mixedReq.ThinkingEffort),
		zap.String("strategy", strategy),
		zap.Bool("partial_success", partialSuccess),
		zap.Bool("is_preview", res.IsPreview),
		zap.Bool("thinking_model", isThinkingModel(chatModel)),
		zap.Bool("thinking_triggered", strings.TrimSpace(res.ReasoningText) != ""),
		zap.Int("reasoning_len", len(strings.TrimSpace(res.ReasoningText))),
	)
	return res, nil
}

func (h *Handler) runMixedModeChatImageConversationStream(
	ctx context.Context,
	taskID string,
	chatModel *modelpkg.Model,
	req *mixedModePreparedRequest,
	sink mixedModeStreamSink,
) (*mixedModeExecResult, *mixedModeAPIError) {
	return h.runMixedModeChatImageConversationCore(ctx, taskID, chatModel, req, sink)
}

func (h *Handler) runMixedModeChatImageConversation(
	ctx context.Context,
	taskID string,
	chatModel *modelpkg.Model,
	req *mixedModePreparedRequest,
) (*mixedModeExecResult, *mixedModeAPIError) {
	return h.runMixedModeChatImageConversationCore(ctx, taskID, chatModel, req, nil)
}

func (h *Handler) runMixedModeChatImageConversationCore(
	ctx context.Context,
	taskID string,
	chatModel *modelpkg.Model,
	req *mixedModePreparedRequest,
	sink mixedModeStreamSink,
) (*mixedModeExecResult, *mixedModeAPIError) {
	lease, cli, cr, excluded, err := h.acquireChatRequirements(ctx, taskID, chatModel)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoAvailable) {
			return nil, &mixedModeAPIError{
				Status:  http.StatusServiceUnavailable,
				Code:    "no_account_available",
				Message: "账号池暂无可用账号,请稍后重试",
			}
		}
		if lease != nil {
			defer func() { _ = lease.Release(context.Background()) }()
			return nil, h.mixedModeUpstreamError(ctx, lease, err)
		}
		return nil, &mixedModeAPIError{
			Status:  http.StatusInternalServerError,
			Code:    "upstream_init_error",
			Message: "上游客户端初始化失败:" + err.Error(),
		}
	}
	defer func() { _ = lease.Release(context.Background()) }()

	if h.Images != nil && h.Images.DAO != nil {
		_ = h.Images.DAO.SetAccount(ctx, taskID, lease.Account.ID)
	}

	proofToken, apiErr := h.solveMixedModeProof(ctx, cr, lease)
	if apiErr != nil {
		return nil, apiErr
	}

	upstreamModel := resolveRequestedUpstreamModel(chatModel)
	if cr.IsFreeAccount() && upstreamModel != "auto" && !isThinkingModel(chatModel) {
		logger.L().Warn("chat mixed image downgrade free account to auto",
			zap.Uint64("account_id", lease.Account.ID),
			zap.String("requested_model_slug", chatModel.Slug),
			zap.String("resolved_upstream_model_slug", upstreamModel))
		upstreamModel = "auto"
	}

	strategy := h.mixedModeImageStrategy(chatModel)
	opt := chatgpt.FChatOpts{
		UpstreamModel:  upstreamModel,
		Messages:       req.Messages,
		ChatToken:      cr.Token,
		ProofToken:     proofToken,
		SSETimeout:     h.sseReadTimeout(),
		ThinkingEffort: req.ThinkingEffort,
		ImageStrategy:  strategy,
	}

	prepCtx, cancelPrep := context.WithTimeout(ctx, 30*time.Second)
	conduit, err := cli.PrepareFChat(prepCtx, opt)
	cancelPrep()
	if err != nil {
		logger.L().Warn("chat mixed image prepare failed, continue without conduit",
			zap.Uint64("account_id", lease.Account.ID),
			zap.String("task_id", taskID),
			zap.Error(err))
	} else {
		opt.ConduitToken = conduit
	}

	stream, err := cli.StreamFChat(ctx, opt)
	if err != nil {
		return nil, h.mixedModeUpstreamError(ctx, lease, err)
	}

	collector := chatgpt.NewStreamMessageCollector()
	evCount := 0
	for ev := range stream {
		if ev.Err != nil {
			return nil, h.mixedModeUpstreamError(ctx, lease, ev.Err)
		}
		if len(ev.Data) == 0 {
			continue
		}
		evCount++
		if evCount <= 16 {
			logger.L().Info("chat mixed image raw",
				zap.String("task_id", taskID),
				zap.Int("n", evCount),
				zap.String("event", ev.Event),
				zap.String("data", truncate(string(ev.Data), 2048)))
		}
		update := collector.Consume(ev.Data)
		if sink != nil {
			if delta := strings.TrimSpace(update.ReasoningDelta); delta != "" {
				sink.OnReasoningDelta(delta)
			}
			if delta := update.AssistantDelta; delta != "" {
				sink.OnAssistantDelta(delta)
			}
		}
		if update.Final {
			break
		}
	}

	finalState := collector.Result()
	res := &mixedModeExecResult{
		TaskID:         taskID,
		AccountID:      lease.Account.ID,
		ConversationID: finalState.ConversationID,
		AssistantText:  finalState.AssistantText,
		ReasoningText:  finalState.ReasoningText,
	}
	reasoningText := strings.TrimSpace(finalState.ReasoningText)

	logger.L().Info("chat mixed image SSE parsed",
		zap.String("task_id", taskID),
		zap.Uint64("account_id", lease.Account.ID),
		zap.String("requested_model_slug", chatModel.Slug),
		zap.String("resolved_upstream_model_slug", upstreamModel),
		zap.String("conversation_id", finalState.ConversationID),
		zap.String("strategy", strategy),
		zap.Int("requested_n", req.RequestedN),
		zap.Int("sse_files", len(finalState.FileIDs)),
		zap.Int("sse_sediments", len(finalState.SedimentIDs)),
		zap.Bool("thinking_model", isThinkingModel(chatModel)),
		zap.Bool("thinking_triggered", reasoningText != ""),
		zap.Int("reasoning_len", len(reasoningText)),
		zap.Int("excluded_account_ids_count", len(excluded)),
		zap.String("persona", cr.Persona),
	)
	fileRefs, isPreview, apiErr := resolveMixedModeFileRefs(
		ctx,
		cli,
		res.ConversationID,
		finalState.FileIDs,
		finalState.SedimentIDs,
		req.RequestedN,
		h.mixedModePollMaxWait(chatModel),
	)
	if apiErr != nil {
		return nil, apiErr
	}
	res.IsPreview = isPreview

	if len(fileRefs) == 0 {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_image_not_returned",
			Message: "上游本轮对话未返回图片结果,没有自动降级到旧图片接口",
		}
	}
	res.FileRefs = make([]string, 0, len(fileRefs))
	res.SignedURLs = make([]string, 0, len(fileRefs))
	res.ContentTypes = make([]string, 0, len(fileRefs))
	res.Images = make([]MixedModeImage, 0, len(fileRefs))
	for _, ref := range fileRefs {
		url, err := cli.ImageDownloadURL(ctx, res.ConversationID, ref)
		if err != nil {
			logger.L().Warn("chat mixed image download url failed",
				zap.String("task_id", taskID),
				zap.String("ref", ref),
				zap.Error(err))
			continue
		}
		idx := len(res.Images)
		contentType := "image/png"
		res.FileRefs = append(res.FileRefs, ref)
		res.SignedURLs = append(res.SignedURLs, url)
		res.ContentTypes = append(res.ContentTypes, contentType)
		res.Images = append(res.Images, MixedModeImage{
			URL:         BuildImageProxyURL(taskID, idx, ImageProxyTTL),
			FileID:      strings.TrimPrefix(ref, "sed:"),
			ContentType: contentType,
			TaskID:      taskID,
			IsPreview:   res.IsPreview,
		})
	}

	if len(res.Images) == 0 {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_image_download_failed",
			Message: "上游已返回图片引用,但签名下载链接获取失败",
		}
	}
	return res, nil
}

func (h *Handler) solveMixedModeProof(
	ctx context.Context,
	cr *chatgpt.ChatRequirementsResp,
	lease *scheduler.Lease,
) (string, *mixedModeAPIError) {
	if cr == nil {
		return "", &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_error",
			Message: "上游 chat requirements 缺失",
		}
	}
	var proofToken string
	if cr.Proofofwork.Required {
		proofCtx, cancelProof := context.WithTimeout(ctx, 5*time.Second)
		defer cancelProof()
		proofCh := make(chan string, 1)
		go func() { proofCh <- cr.SolveProof("") }()

		select {
		case <-proofCtx.Done():
			h.Scheduler.MarkWarned(context.Background(), lease.Account.ID)
			return "", &mixedModeAPIError{
				Status:  http.StatusServiceUnavailable,
				Code:    "pow_timeout",
				Message: "上游风控(PoW)未在规定时间内完成,请重试",
			}
		case proofToken = <-proofCh:
		}
		if proofToken == "" {
			h.Scheduler.MarkWarned(context.Background(), lease.Account.ID)
			return "", &mixedModeAPIError{
				Status:  http.StatusServiceUnavailable,
				Code:    "pow_failed",
				Message: "上游风控(PoW)校验失败,请稍后重试",
			}
		}
	}
	if cr.Turnstile.Required {
		logger.L().Warn("chat mixed image turnstile required, continue anyway",
			zap.Uint64("account_id", lease.Account.ID))
	}
	return proofToken, nil
}
