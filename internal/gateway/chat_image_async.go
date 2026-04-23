package gateway

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/432539/gpt2api/internal/image"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/scheduler"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
	"github.com/432539/gpt2api/pkg/logger"
	"go.uber.org/zap"
)

type mixedModeAsyncState struct {
	RequestID     string
	UserID        uint64
	KeyID         uint64
	ClientIP      string
	EstimatedCost int64
	StartedAt     time.Time
	ComputeCost   func(int) int64
}

func buildMixedModeImageTask(taskID, conversationID string, requestedN, readyN int, status string) *MixedModeImageTask {
	if strings.TrimSpace(taskID) == "" {
		return nil
	}
	if requestedN <= 0 {
		requestedN = 1
	}
	if readyN < 0 {
		readyN = 0
	}
	return &MixedModeImageTask{
		TaskID:         taskID,
		Status:         status,
		RequestedN:     requestedN,
		ReadyN:         readyN,
		ConversationID: conversationID,
	}
}

func buildMixedModeImages(taskID string, refs []string, isPreview bool) ([]MixedModeImage, []string, []string) {
	images := make([]MixedModeImage, 0, len(refs))
	resultURLs := make([]string, 0, len(refs))
	contentTypes := make([]string, 0, len(refs))
	for idx, ref := range refs {
		url := BuildImageProxyURL(taskID, idx, ImageProxyTTL)
		contentType := "image/png"
		images = append(images, MixedModeImage{
			URL:         url,
			FileID:      strings.TrimPrefix(ref, "sed:"),
			ContentType: contentType,
			TaskID:      taskID,
			IsPreview:   isPreview,
		})
		resultURLs = append(resultURLs, url)
		contentTypes = append(contentTypes, contentType)
	}
	return images, resultURLs, contentTypes
}

func mixedModeIsPreview(refs []string) bool {
	if len(refs) == 0 {
		return false
	}
	for _, ref := range refs {
		if !strings.HasPrefix(ref, "sed:") {
			return false
		}
	}
	return true
}

func extractMixedModeRefsFromMapping(mapping map[string]interface{}, limit int) []string {
	rawMapping, _ := mapping["mapping"].(map[string]interface{})
	if rawMapping == nil {
		return nil
	}
	refs := make([]string, 0, 4)
	for _, msg := range chatgpt.ExtractImageToolMsgs(rawMapping) {
		refs = mergeMixedModeRefs(refs, msg.FileIDs, msg.SedimentIDs, limit)
	}
	return refs
}

func mergeMixedModeRefList(existing, extra []string, limit int) []string {
	refs := make([]string, 0, len(existing)+len(extra))
	seen := make(map[string]struct{}, len(existing)+len(extra))
	appendRef := func(ref string) {
		if ref == "" {
			return
		}
		if _, ok := seen[ref]; ok {
			return
		}
		seen[ref] = struct{}{}
		refs = append(refs, ref)
	}
	for _, ref := range existing {
		appendRef(ref)
	}
	for _, ref := range extra {
		appendRef(ref)
	}
	if limit > 0 && len(refs) > limit {
		return refs[:limit]
	}
	return refs
}

func (h *Handler) recoverMixedModeImageRefs(
	ctx context.Context,
	cli *chatgpt.Client,
	conversationID string,
	initialRefs []string,
	accepted bool,
	requestedN int,
	maxWait time.Duration,
	onProgress func([]string),
) ([]string, bool, error) {
	refs := append([]string(nil), initialRefs...)
	if len(refs) > 0 {
		accepted = true
	}
	if requestedN <= 0 {
		requestedN = 1
	}
	if len(refs) >= requestedN || cli == nil || strings.TrimSpace(conversationID) == "" || maxWait <= 0 {
		return refs, accepted, nil
	}

	deadline := time.Now().Add(maxWait)
	lastReady := len(refs)
	notifyProgress := func() {
		if onProgress == nil || len(refs) <= lastReady {
			return
		}
		lastReady = len(refs)
		onProgress(append([]string(nil), refs...))
	}

	for {
		if err := ctx.Err(); err != nil {
			return refs, accepted, err
		}
		if len(refs) >= requestedN || time.Now().After(deadline) {
			return refs, accepted, nil
		}

		statusCtx, cancelStatus := context.WithTimeout(ctx, 15*time.Second)
		status, statusErr := cli.GetConversationStreamStatus(statusCtx, conversationID)
		cancelStatus()
		if statusErr == nil && strings.TrimSpace(status) != "" {
			accepted = true
		}

		mapCtx, cancelMap := context.WithTimeout(ctx, 20*time.Second)
		mapping, mapErr := cli.GetConversationMapping(mapCtx, conversationID)
		cancelMap()
		if mapErr == nil {
			nextRefs := extractMixedModeRefsFromMapping(mapping, requestedN)
			if len(nextRefs) > 0 {
				accepted = true
				refs = mergeMixedModeRefList(refs, nextRefs, requestedN)
				notifyProgress()
			}
		}

		if len(refs) >= requestedN {
			return refs, accepted, nil
		}
		if time.Now().After(deadline) {
			return refs, accepted, nil
		}
		if statusErr != nil && mapErr != nil && !errors.Is(statusErr, context.Canceled) && !errors.Is(mapErr, context.Canceled) {
			logger.L().Debug("chat mixed image recovery tick failed",
				zap.String("conversation_id", conversationID),
				zap.NamedError("stream_status_error", statusErr),
				zap.NamedError("mapping_error", mapErr))
		}
		select {
		case <-ctx.Done():
			return refs, accepted, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func (h *Handler) startMixedModeAsyncRecovery(
	async *mixedModeAsyncState,
	lease *scheduler.Lease,
	cli *chatgpt.Client,
	chatModel *modelpkg.Model,
	req *mixedModePreparedRequest,
	seed *mixedModeExecResult,
) {
	if async == nil || lease == nil || cli == nil || req == nil || seed == nil {
		if lease != nil {
			_ = lease.Release(context.Background())
		}
		return
	}

	backgroundWait := h.mixedModePollMaxWait(chatModel) - h.mixedModeBlockingWait(chatModel)
	if backgroundWait < 5*time.Second {
		backgroundWait = 5 * time.Second
	}

	go func() {
		defer func() { _ = lease.Release(context.Background()) }()

		bgCtx, cancel := context.WithTimeout(context.Background(), backgroundWait)
		defer cancel()

		progress := func(refs []string) {
			images, resultURLs, _ := buildMixedModeImages(seed.TaskID, refs, mixedModeIsPreview(refs))
			seed.FileRefs = append([]string(nil), refs...)
			seed.Images = images
			seed.ImageTask = buildMixedModeImageTask(seed.TaskID, seed.ConversationID, req.RequestedN, len(images), image.StatusRunning)
			if h.Images != nil && h.Images.DAO != nil {
				_ = h.Images.DAO.UpdateProgress(bgCtx, seed.TaskID, seed.ConversationID, refs, resultURLs)
			}
		}

		refs, accepted, err := h.recoverMixedModeImageRefs(
			bgCtx,
			cli,
			seed.ConversationID,
			seed.FileRefs,
			true,
			req.RequestedN,
			backgroundWait,
			progress,
		)
		if err == nil && accepted && len(refs) >= req.RequestedN {
			seed.Status = mixedModeExecStatusCompleted
			seed.IsPreview = mixedModeIsPreview(refs)
			seed.FileRefs = append([]string(nil), refs...)
			seed.Images, seed.SignedURLs, seed.ContentTypes = buildMixedModeImages(seed.TaskID, refs, seed.IsPreview)
			seed.ImageTask = buildMixedModeImageTask(seed.TaskID, seed.ConversationID, req.RequestedN, len(seed.Images), image.StatusSuccess)

			actualCost := int64(0)
			if async.ComputeCost != nil {
				actualCost = async.ComputeCost(len(seed.Images))
			}
			if err := h.Billing.Settle(context.Background(), async.UserID, async.KeyID, async.EstimatedCost, actualCost, async.RequestID, "chat image mixed settle"); err != nil {
				logger.L().Error("billing settle chat mixed image async", zap.Error(err), zap.String("ref", async.RequestID))
			}
			h.touchKeyUsage(context.Background(), async.KeyID, async.ClientIP, actualCost)

			if h.Images != nil && h.Images.DAO != nil {
				_ = h.Images.DAO.MarkSuccess(context.Background(), seed.TaskID, seed.ConversationID, seed.FileRefs, seed.taskResultURLs(), actualCost)
				_ = h.Images.DAO.UpdateCost(context.Background(), seed.TaskID, actualCost)
			}
			if h.Usage != nil {
				_ = h.Usage.Finalize(context.Background(), async.RequestID, usage.FinalizePatch{
					AccountID:  seed.AccountID,
					ImageCount: len(seed.Images),
					CreditCost: actualCost,
					DurationMs: int(time.Since(async.StartedAt).Milliseconds()),
					Status:     usage.StatusSuccess,
				})
			}
			logger.L().Info("chat mixed image async success",
				zap.String("request_id", async.RequestID),
				zap.String("task_id", seed.TaskID),
				zap.Uint64("account_id", seed.AccountID),
				zap.String("conversation_id", seed.ConversationID),
				zap.Int("requested_n", req.RequestedN),
				zap.Int("actual_n", len(seed.Images)))
			return
		}

		if h.Images != nil && h.Images.DAO != nil {
			_ = h.Images.DAO.MarkFailed(context.Background(), seed.TaskID, "upstream_image_not_returned")
		}
		if async.EstimatedCost > 0 {
			_ = h.Billing.Refund(context.Background(), async.UserID, async.KeyID, async.EstimatedCost, async.RequestID, "chat image mixed refund")
		}
		if h.Usage != nil {
			_ = h.Usage.Finalize(context.Background(), async.RequestID, usage.FinalizePatch{
				AccountID:  seed.AccountID,
				ImageCount: len(seed.Images),
				CreditCost: 0,
				DurationMs: int(time.Since(async.StartedAt).Milliseconds()),
				Status:     usage.StatusFailed,
				ErrorCode:  "upstream_image_not_returned",
			})
		}
		logger.L().Warn("chat mixed image async failed",
			zap.String("request_id", async.RequestID),
			zap.String("task_id", seed.TaskID),
			zap.String("conversation_id", seed.ConversationID),
			zap.Int("requested_n", req.RequestedN),
			zap.Int("actual_n", len(seed.FileRefs)),
			zap.Bool("accepted", accepted),
			zap.Error(err))
	}()
}
