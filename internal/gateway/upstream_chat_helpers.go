package gateway

import (
	"context"
	"time"

	"go.uber.org/zap"

	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/scheduler"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/pkg/logger"
)

func dispatchChatOptions(modelType string, requirePaid bool, excluded map[uint64]struct{}) scheduler.DispatchOptions {
	if excluded == nil {
		excluded = map[uint64]struct{}{}
	}
	return scheduler.DispatchOptions{
		ModelType:         modelType,
		RequirePaid:       requirePaid,
		ExcludeAccountIDs: excluded,
	}
}

func (h *Handler) newChatGPTClient(ctx context.Context, lease *scheduler.Lease) (*chatgpt.Client, error) {
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
		return nil, err
	}
	bootCtx, cancelBoot := context.WithTimeout(ctx, 15*time.Second)
	defer cancelBoot()
	if err := cli.Bootstrap(bootCtx); err != nil {
		logger.L().Warn("chat bootstrap failed, continue anyway",
			zap.Uint64("account_id", lease.Account.ID),
			zap.Error(err))
	}
	return cli, nil
}

func (h *Handler) markLeaseAsKnownFree(ctx context.Context, requestID string, lease *scheduler.Lease, excluded map[uint64]struct{}) {
	if lease == nil || lease.Account == nil {
		return
	}
	h.markFreeAccount(ctx, lease.Account.ID)
	if excluded != nil {
		excluded[lease.Account.ID] = struct{}{}
	}
	logger.L().Warn("thinking request hit free persona, retry with another account",
		zap.String("request_id", requestID),
		zap.Uint64("account_id", lease.Account.ID),
		zap.String("retry_reason", "free_persona"),
		zap.Int("excluded_account_ids_count", len(excluded)))
	_ = h.abortLease(context.Background(), lease)
}

func thinkingDispatchRequired(chatModel *modelpkg.Model) bool {
	return isThinkingModel(chatModel)
}

func (h *Handler) acquireChatRequirements(
	ctx context.Context,
	requestID string,
	chatModel *modelpkg.Model,
) (*scheduler.Lease, *chatgpt.Client, *chatgpt.ChatRequirementsResp, map[uint64]struct{}, error) {
	excluded := map[uint64]struct{}{}
	requirePaid := thinkingDispatchRequired(chatModel)

	for {
		lease, err := h.dispatchChatLease(ctx, dispatchChatOptions(modelpkg.TypeChat, requirePaid, excluded))
		if err != nil {
			return nil, nil, nil, excluded, err
		}

		cli, cr, err := h.loadChatRequirements(ctx, lease)
		if err != nil {
			if cli == nil {
				_ = h.abortLease(context.Background(), lease)
			}
			return lease, cli, nil, excluded, err
		}
		if requirePaid && cr.IsFreeAccount() {
			h.markLeaseAsKnownFree(context.Background(), requestID, lease, excluded)
			continue
		}
		return lease, cli, cr, excluded, nil
	}
}
