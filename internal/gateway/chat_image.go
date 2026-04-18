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

const (
	mixedModeImageEstCount = 4
	mixedModeRunTimeout    = 6 * time.Minute
	mixedModePollMaxWait   = 180 * time.Second
)

type mixedModeExecResult struct {
	TaskID         string
	AccountID      uint64
	ConversationID string
	FileRefs       []string
	SignedURLs     []string
	ContentTypes   []string
	Images         []MixedModeImage
	IsPreview      bool
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

func (h *Handler) handleChatImageGeneration(c *gin.Context, rec *usage.Log, ak *apikey.APIKey, req *ChatCompletionsRequest) {
	if req.Stream {
		rec.Status = usage.StatusFailed
		rec.ErrorCode = "image_generation_stream_unsupported"
		openAIError(c, http.StatusBadRequest, "image_generation_stream_unsupported",
			"chat mixed-mode 生图首版暂不支持 stream=true")
		return
	}

	res, apiErr := h.callMixedModeChatImage(c, rec, ak, req.Model, req.Messages)
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
				Content: "",
			},
			FinishReason: "stop",
		}},
		Usage:  ChatCompletionUsage{},
		Images: res.Images,
	})
}

func (h *Handler) executeMixedModeChatImage(
	c *gin.Context,
	rec *usage.Log,
	ak *apikey.APIKey,
	requestedModel string,
	messages []chatgpt.ChatMessage,
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

	prompt := extractLastUserPrompt(messages)
	if strings.TrimSpace(prompt) == "" {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request_error",
			Message: "image_generation=true 时,最后一条 user 消息必须提供生图提示词",
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
			Message: fmt.Sprintf("模型 %q 不存在或已下架", requestedModel),
		}
	}
	if chatModel.Type != modelpkg.TypeChat {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadRequest,
			Code:    "model_type_mismatch",
			Message: "image_generation=true 仅支持 chat/thinking 模型,不支持直接传图片模型",
		}
	}

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
	estimatedCost := billing.ComputeImageCost(imageModel, mixedModeImageEstCount, ratio)
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

	taskID := image.GenerateTaskID()
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
		Prompt:          prompt,
		N:               1,
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

	runCtx, cancel := context.WithTimeout(c.Request.Context(), mixedModeRunTimeout)
	defer cancel()

	logger.L().Info("chat mixed image start",
		zap.String("request_id", rec.RequestID),
		zap.String("chat_model", requestedModel),
		zap.String("task_id", taskID),
	)

	res, apiErr := h.callMixedModeConversation(runCtx, taskID, chatModel, messages)
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

	logger.L().Info("chat mixed image success",
		zap.String("request_id", rec.RequestID),
		zap.String("task_id", taskID),
		zap.Uint64("account_id", res.AccountID),
		zap.String("conversation_id", res.ConversationID),
		zap.Int("images", len(res.Images)),
		zap.Bool("is_preview", res.IsPreview),
	)
	return res, nil
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

func (h *Handler) runMixedModeChatImageConversation(
	ctx context.Context,
	taskID string,
	chatModel *modelpkg.Model,
	messages []chatgpt.ChatMessage,
) (*mixedModeExecResult, *mixedModeAPIError) {
	lease, err := h.Scheduler.Dispatch(ctx, modelpkg.TypeChat)
	if err != nil {
		if errors.Is(err, scheduler.ErrNoAvailable) {
			return nil, &mixedModeAPIError{
				Status:  http.StatusServiceUnavailable,
				Code:    "no_account_available",
				Message: "账号池暂无可用账号,请稍后重试",
			}
		}
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_error",
			Message: "调度上游账号失败:" + err.Error(),
		}
	}
	defer func() { _ = lease.Release(context.Background()) }()

	if h.Images != nil && h.Images.DAO != nil {
		_ = h.Images.DAO.SetAccount(ctx, taskID, lease.Account.ID)
	}

	cookies, _ := h.AccSvc.DecryptCookies(ctx, lease.Account.ID)
	cli, err := chatgpt.New(chatgpt.Options{
		AuthToken:  lease.AuthToken,
		DeviceID:   lease.DeviceID,
		SessionID:  lease.SessionID,
		ProxyURL:   lease.ProxyURL,
		Cookies:    cookies,
		Timeout:    h.upstreamTimeout(),
		SSETimeout: h.sseReadTimeout(),
	})
	if err != nil {
		return nil, &mixedModeAPIError{
			Status:  http.StatusInternalServerError,
			Code:    "upstream_init_error",
			Message: "上游客户端初始化失败:" + err.Error(),
		}
	}

	bootCtx, cancelBoot := context.WithTimeout(ctx, 15*time.Second)
	_ = cli.Bootstrap(bootCtx)
	cancelBoot()

	cr, proofToken, apiErr := h.prepareMixedModeRequirements(ctx, cli, lease)
	if apiErr != nil {
		return nil, apiErr
	}

	upstreamModel := chatModel.UpstreamModelSlug
	if upstreamModel == "" {
		upstreamModel = "auto"
	}
	upstreamModel = mapUpstreamModelSlug(upstreamModel)
	if cr.IsFreeAccount() && upstreamModel != "auto" {
		logger.L().Warn("chat mixed image downgrade free account to auto",
			zap.Uint64("account_id", lease.Account.ID),
			zap.String("requested_model", upstreamModel))
		upstreamModel = "auto"
	}

	upstreamMessages := cloneMessagesForImageTool(messages)
	opt := chatgpt.FChatOpts{
		UpstreamModel:   upstreamModel,
		Messages:        upstreamMessages,
		ChatToken:       cr.Token,
		ProofToken:      proofToken,
		SSETimeout:      h.sseReadTimeout(),
		EnableImageTool: true,
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
	sseRes := chatgpt.ParseImageSSE(stream)

	res := &mixedModeExecResult{
		TaskID:         taskID,
		AccountID:      lease.Account.ID,
		ConversationID: sseRes.ConversationID,
	}

	logger.L().Info("chat mixed image SSE parsed",
		zap.String("task_id", taskID),
		zap.Uint64("account_id", lease.Account.ID),
		zap.String("conversation_id", sseRes.ConversationID),
		zap.Int("sse_files", len(sseRes.FileIDs)),
		zap.Int("sse_sediments", len(sseRes.SedimentIDs)),
	)

	fileRefs := make([]string, 0, len(sseRes.FileIDs)+len(sseRes.SedimentIDs))
	if len(sseRes.FileIDs) > 0 {
		fileRefs = append(fileRefs, sseRes.FileIDs...)
		for _, sid := range sseRes.SedimentIDs {
			fileRefs = append(fileRefs, "sed:"+sid)
		}
	} else {
		if res.ConversationID == "" {
			return nil, &mixedModeAPIError{
				Status:  http.StatusBadGateway,
				Code:    "upstream_image_not_returned",
				Message: "上游本轮对话未返回可追踪的图片会话,请重试",
			}
		}
		status, fids, sids := cli.PollConversationForImages(ctx, res.ConversationID, chatgpt.PollOpts{
			MaxWait: mixedModePollMaxWait,
		})
		switch status {
		case chatgpt.PollStatusIMG2:
			fileRefs = append(fileRefs, fids...)
			for _, sid := range sids {
				fileRefs = append(fileRefs, "sed:"+sid)
			}
		case chatgpt.PollStatusPreviewOnly:
			res.IsPreview = true
			fileRefs = append(fileRefs, fids...)
			for _, sid := range sids {
				fileRefs = append(fileRefs, "sed:"+sid)
			}
		case chatgpt.PollStatusTimeout:
			return nil, &mixedModeAPIError{
				Status:  http.StatusBadGateway,
				Code:    "upstream_image_not_returned",
				Message: "上游会话在规定时间内没有返回图片结果,请稍后重试",
			}
		default:
			return nil, &mixedModeAPIError{
				Status:  http.StatusBadGateway,
				Code:    "upstream_error",
				Message: "上游图片结果轮询失败,请稍后重试",
			}
		}
	}

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

func (h *Handler) prepareMixedModeRequirements(
	ctx context.Context,
	cli *chatgpt.Client,
	lease *scheduler.Lease,
) (*chatgpt.ChatRequirementsResp, string, *mixedModeAPIError) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cr, err := cli.ChatRequirementsV2(reqCtx)
	if err != nil {
		return nil, "", h.mixedModeUpstreamError(ctx, lease, err)
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
			return nil, "", &mixedModeAPIError{
				Status:  http.StatusServiceUnavailable,
				Code:    "pow_timeout",
				Message: "上游风控(PoW)未在规定时间内完成,请重试",
			}
		case proofToken = <-proofCh:
		}
		if proofToken == "" {
			h.Scheduler.MarkWarned(context.Background(), lease.Account.ID)
			return nil, "", &mixedModeAPIError{
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
	return cr, proofToken, nil
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
			h.Scheduler.MarkRateLimited(context.Background(), lease.Account.ID)
		case ue.IsUnauthorized():
			h.Scheduler.MarkDead(context.Background(), lease.Account.ID)
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

func cloneMessagesForImageTool(messages []chatgpt.ChatMessage) []chatgpt.ChatMessage {
	out := make([]chatgpt.ChatMessage, len(messages))
	copy(out, messages)
	for i := len(out) - 1; i >= 0; i-- {
		if out[i].Role == "user" && strings.TrimSpace(out[i].Content) != "" {
			out[i].Content = maybeAppendClaritySuffix(out[i].Content)
			break
		}
	}
	return out
}
