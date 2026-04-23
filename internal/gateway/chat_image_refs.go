package gateway

import (
	"context"
	"net/http"
	"time"

	"github.com/432539/gpt2api/internal/upstream/chatgpt"
)

type mixedModeImagePoller interface {
	PollConversationForImages(ctx context.Context, convID string, opt chatgpt.PollOpts) (chatgpt.PollStatus, []string, []string)
}

func collectMixedModeRefs(fileIDs, sedimentIDs []string) []string {
	refs := make([]string, 0, len(fileIDs)+len(sedimentIDs))
	refs = append(refs, fileIDs...)
	for _, sid := range sedimentIDs {
		refs = append(refs, "sed:"+sid)
	}
	return refs
}

func mergeMixedModeRefs(existing []string, fileIDs, sedimentIDs []string, limit int) []string {
	refs := make([]string, 0, len(existing)+len(fileIDs)+len(sedimentIDs))
	seen := make(map[string]struct{}, len(existing)+len(fileIDs)+len(sedimentIDs))
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
	for _, ref := range collectMixedModeRefs(fileIDs, sedimentIDs) {
		appendRef(ref)
	}
	if limit > 0 && len(refs) > limit {
		return refs[:limit]
	}
	return refs
}

func resolveMixedModeFileRefs(
	ctx context.Context,
	poller mixedModeImagePoller,
	conversationID string,
	initialFileIDs, initialSedimentIDs []string,
	requestedN int,
	maxWait time.Duration,
) ([]string, bool, *mixedModeAPIError) {
	if requestedN <= 0 {
		requestedN = 1
	}
	initialRefs := collectMixedModeRefs(initialFileIDs, initialSedimentIDs)
	isPreview := len(initialFileIDs) == 0 && len(initialSedimentIDs) > 0
	if len(initialRefs) >= requestedN {
		return initialRefs[:requestedN], isPreview, nil
	}
	if conversationID == "" {
		if len(initialRefs) > 0 {
			return initialRefs, isPreview, nil
		}
		return nil, false, &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_image_not_returned",
			Message: "上游本轮对话未返回可追踪的图片会话,请重试",
		}
	}
	status, fileIDs, sedimentIDs := poller.PollConversationForImages(ctx, conversationID, chatgpt.PollOpts{
		MaxWait:     maxWait,
		TargetCount: requestedN,
	})
	merged := mergeMixedModeRefs(initialRefs, fileIDs, sedimentIDs, requestedN)
	switch status {
	case chatgpt.PollStatusIMG2:
		return merged, false, nil
	case chatgpt.PollStatusPreviewOnly:
		return merged, true, nil
	case chatgpt.PollStatusTimeout, chatgpt.PollStatusError:
		if len(merged) > 0 {
			return merged, isPreview, nil
		}
		if status == chatgpt.PollStatusTimeout {
			return nil, false, &mixedModeAPIError{
				Status:  http.StatusBadGateway,
				Code:    "upstream_image_not_returned",
				Message: "上游会话在规定时间内没有返回图片结果,请稍后重试",
			}
		}
		return nil, false, &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_error",
			Message: "上游图片结果轮询失败,请稍后重试",
		}
	default:
		if len(merged) > 0 {
			return merged, isPreview, nil
		}
		return nil, false, &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_error",
			Message: "上游图片结果轮询失败,请稍后重试",
		}
	}
}
