package gateway

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/image"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/usage"
	"github.com/432539/gpt2api/internal/user"
)

type modelStore interface {
	BySlug(ctx context.Context, slug string) (*modelpkg.Model, error)
	ListEnabled(ctx context.Context) ([]*modelpkg.Model, error)
}

type billingStore interface {
	PreDeduct(ctx context.Context, userID, keyID uint64, amount int64, refID, remark string) error
	Settle(ctx context.Context, userID, keyID uint64, expected, actual int64, refID, remark string) error
	Refund(ctx context.Context, userID, keyID uint64, expected int64, refID, remark string) error
}

type limiterStore interface {
	AllowRPM(ctx context.Context, keyID uint64, capacity int) (bool, float64, error)
	AllowTPM(ctx context.Context, keyID uint64, capacity int64, tokens int64) (bool, float64, error)
	AdjustTPM(ctx context.Context, keyID uint64, capacity int64, delta int64)
}

type groupStore interface {
	OfUser(ctx context.Context, userID uint64) (*user.Group, error)
}

type keyStore interface {
	TouchUsage(ctx context.Context, id uint64, lastIP string, deltaQuota int64) error
}

type usageStore interface {
	Write(row *usage.Log)
}

type imageTaskStore interface {
	Create(ctx context.Context, t *image.Task) error
	SetAccount(ctx context.Context, taskID string, accountID uint64) error
	MarkSuccess(ctx context.Context, taskID, convID string, fileIDs, resultURLs []string, creditCost int64) error
	UpdateCost(ctx context.Context, taskID string, cost int64) error
	MarkFailed(ctx context.Context, taskID, errorCode string) error
	Get(ctx context.Context, taskID string) (*image.Task, error)
}

type mixedModeExecFunc func(
	c *gin.Context,
	rec *usage.Log,
	ak *apikey.APIKey,
	requestedModel string,
	input mixedModeRequestInput,
) (*mixedModeExecResult, *mixedModeAPIError)

type mixedModeConversationRunnerFunc func(
	ctx context.Context,
	taskID string,
	chatModel *modelpkg.Model,
	req *mixedModePreparedRequest,
) (*mixedModeExecResult, *mixedModeAPIError)

var (
	nowFunc     = time.Now
	newUUIDFunc = uuid.NewString
)

func (h *Handler) writeUsage(rec *usage.Log) {
	if h.Usage != nil {
		h.Usage.Write(rec)
	}
}

func (h *Handler) touchKeyUsage(ctx context.Context, keyID uint64, ip string, cost int64) {
	if h.Keys == nil {
		return
	}
	_ = h.Keys.TouchUsage(ctx, keyID, ip, cost)
}

func (h *Handler) callMixedModeChatImage(
	c *gin.Context,
	rec *usage.Log,
	ak *apikey.APIKey,
	requestedModel string,
	input mixedModeRequestInput,
) (*mixedModeExecResult, *mixedModeAPIError) {
	if h.mixedModeExec != nil {
		return h.mixedModeExec(c, rec, ak, requestedModel, input)
	}
	return h.executeMixedModeChatImage(c, rec, ak, requestedModel, input)
}

func (h *Handler) callMixedModeConversation(
	ctx context.Context,
	taskID string,
	chatModel *modelpkg.Model,
	req *mixedModePreparedRequest,
) (*mixedModeExecResult, *mixedModeAPIError) {
	if h.mixedModeConversationRunner != nil {
		return h.mixedModeConversationRunner(ctx, taskID, chatModel, req)
	}
	return h.runMixedModeChatImageConversation(ctx, taskID, chatModel, req)
}
