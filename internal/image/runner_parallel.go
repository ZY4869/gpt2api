package image

import (
	"context"
	"errors"
	"sync"

	"go.uber.org/zap"

	"github.com/432539/gpt2api/internal/scheduler"
	"github.com/432539/gpt2api/pkg/logger"
)

type parallelSubResult struct {
	ok     bool
	status string
	err    error
	result *RunResult
}

func (r *Runner) runParallel(ctx context.Context, opt RunOptions, result *RunResult) (bool, string, error) {
	lease, err := r.sched.Dispatch(ctx, scheduler.DispatchOptions{ModelType: "image"})
	if err != nil {
		if errors.Is(err, scheduler.ErrNoAvailable) {
			return false, ErrNoAccount, err
		}
		return false, ErrUnknown, err
	}
	defer func() {
		_ = lease.Release(context.Background())
	}()

	result.AccountID = lease.Account.ID
	if r.dao != nil && opt.TaskID != "" {
		_ = r.dao.SetAccount(ctx, opt.TaskID, lease.Account.ID)
	}

	subOpt := opt
	subOpt.N = 1
	subOpt.TaskID = ""

	ch := make(chan parallelSubResult, opt.N)
	var wg sync.WaitGroup

	for i := 0; i < opt.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subRes := &RunResult{Status: StatusFailed, ErrorCode: ErrUnknown}
			ok, status, runErr := r.runWithLease(ctx, subOpt, subRes, lease)
			ch <- parallelSubResult{ok: ok, status: status, err: runErr, result: subRes}
		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	var (
		successCount int
		previewCount int
		lastStatus   string
		lastErr      error
	)

	for sub := range ch {
		if sub.ok {
			successCount++
			if sub.result != nil {
				result.FileIDs = append(result.FileIDs, sub.result.FileIDs...)
				result.SignedURLs = append(result.SignedURLs, sub.result.SignedURLs...)
				result.ContentTypes = append(result.ContentTypes, sub.result.ContentTypes...)
				if result.ConversationID == "" {
					result.ConversationID = sub.result.ConversationID
				}
				if sub.result.IsPreview {
					previewCount++
				}
			}
			continue
		}
		if sub.status != "" {
			lastStatus = sub.status
		}
		if sub.err != nil {
			lastErr = sub.err
		}
	}

	if successCount > 0 {
		result.IsPreview = previewCount > 0
		logger.L().Info("image runner parallel done",
			zap.String("task_id", opt.TaskID),
			zap.Uint64("account_id", lease.Account.ID),
			zap.Int("requested", opt.N),
			zap.Int("succeeded", successCount),
			zap.Int("got_images", len(result.FileIDs)),
			zap.Int("preview_results", previewCount),
		)
		return true, "", nil
	}

	if lastStatus == "" {
		lastStatus = ErrUnknown
	}
	logger.L().Warn("image runner parallel failed",
		zap.String("task_id", opt.TaskID),
		zap.Uint64("account_id", lease.Account.ID),
		zap.Int("requested", opt.N),
		zap.String("retry_reason", lastStatus),
	)
	return false, lastStatus, lastErr
}
