package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/billing"
	"github.com/432539/gpt2api/internal/image"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
	"github.com/432539/gpt2api/pkg/logger"
)

func TestExecuteMixedModeChatImageSuccessDefaultsToSingleImage(t *testing.T) {
	chatModel := &modelpkg.Model{ID: 11, Slug: "gpt-5", Type: modelpkg.TypeChat, Enabled: true, UpstreamModelSlug: "gpt-5"}
	imageModel := &modelpkg.Model{ID: 22, Slug: "gpt-image-2", Type: modelpkg.TypeImage, Enabled: true, ImagePricePerCall: 440}
	models := &fakeModelStore{
		bySlug:  map[string]*modelpkg.Model{"gpt-5": chatModel, "gpt-image-2": imageModel},
		enabled: []*modelpkg.Model{chatModel, imageModel},
	}
	bill := &fakeBillingStore{}
	keyDAO := &fakeKeyDAO{}
	taskStore := &fakeImageTaskStore{}
	h := &Handler{
		Models:   models,
		Billing:  bill,
		Keys:     fakeKeyStore{dao: keyDAO},
		Settings: fakeSettings{mixedEnabled: true},
	}
	h.mixedModeConversationRunner = func(_ context.Context, taskID string, _ *modelpkg.Model, req *mixedModePreparedRequest) (*mixedModeExecResult, *mixedModeAPIError) {
		if req.RequestedN != 1 {
			t.Fatalf("requested_n = %d, want 1", req.RequestedN)
		}
		if !req.WaitForResult {
			t.Fatalf("wait_for_result = %v, want true", req.WaitForResult)
		}
		return &mixedModeExecResult{
			TaskID:         taskID,
			AccountID:      77,
			ConversationID: "conv_mixed_1",
			FileRefs:       []string{"file_a"},
			SignedURLs:     []string{"https://signed.example/0"},
			Images: []MixedModeImage{
				{URL: "/p/img/task/0?exp=1&sig=a", FileID: "file_a", ContentType: "image/png", TaskID: taskID},
			},
		}, nil
	}
	h.Images = &ImagesHandler{
		Handler:          h,
		DAO:              taskStore,
		ImageAccResolver: stubImageAccountResolver{},
	}

	rec := &usage.Log{RequestID: "req_mixed_success"}
	ak := &apikey.APIKey{ID: 2, UserID: 3, Enabled: true}
	c, _ := newJSONContext(t, "/v1/chat/completions", `{}`, ak)
	res, apiErr := h.executeMixedModeChatImage(c, rec, ak, "gpt-5", mixedModeRequestInput{
		Messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
	})
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if res == nil || len(res.Images) != 1 {
		t.Fatalf("images = %d, want 1", len(res.Images))
	}
	if len(taskStore.created) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(taskStore.created))
	}
	if taskStore.created[0].ModelID != imageModel.ID {
		t.Fatalf("created model_id = %d, want %d", taskStore.created[0].ModelID, imageModel.ID)
	}
	if taskStore.created[0].N != 1 {
		t.Fatalf("created n = %d, want 1", taskStore.created[0].N)
	}
	if taskStore.success.ConvID != "conv_mixed_1" {
		t.Fatalf("conversation_id = %s, want conv_mixed_1", taskStore.success.ConvID)
	}
	if len(taskStore.success.FileIDs) != 1 || taskStore.success.FileIDs[0] != "file_a" {
		t.Fatalf("file ids = %#v", taskStore.success.FileIDs)
	}
	if len(bill.preDeducts) != 1 || bill.preDeducts[0].Amount != 440 {
		t.Fatalf("prededuct = %#v, want amount 440", bill.preDeducts)
	}
	if len(bill.settles) != 1 || bill.settles[0].Expected != 440 || bill.settles[0].Actual != 440 {
		t.Fatalf("settles = %#v", bill.settles)
	}
	if len(bill.refunds) != 0 {
		t.Fatalf("refunds = %#v, want none", bill.refunds)
	}
	if len(keyDAO.touches) != 1 || keyDAO.touches[0].Cost != 440 {
		t.Fatalf("key touches = %#v", keyDAO.touches)
	}
	if rec.Status != usage.StatusSuccess || rec.Type != usage.TypeImage {
		t.Fatalf("rec status/type = %s/%s", rec.Status, rec.Type)
	}
	if rec.AccountID != 77 || rec.ImageCount != 1 || rec.CreditCost != 440 {
		t.Fatalf("rec usage fields = %+v", *rec)
	}
}

func TestExecuteMixedModeChatImageSuccessN3SettlesRequestedCount(t *testing.T) {
	chatModel := &modelpkg.Model{ID: 11, Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}
	imageModel := &modelpkg.Model{ID: 22, Slug: "gpt-image-2", Type: modelpkg.TypeImage, Enabled: true, ImagePricePerCall: 300}
	taskStore := &fakeImageTaskStore{}
	bill := &fakeBillingStore{}
	h := &Handler{
		Models: &fakeModelStore{
			bySlug:  map[string]*modelpkg.Model{"gpt-5-thinking": chatModel, "gpt-image-2": imageModel},
			enabled: []*modelpkg.Model{chatModel, imageModel},
		},
		Billing:  bill,
		Settings: fakeSettings{mixedEnabled: true},
	}
	h.mixedModeConversationRunner = func(_ context.Context, taskID string, _ *modelpkg.Model, req *mixedModePreparedRequest) (*mixedModeExecResult, *mixedModeAPIError) {
		if req.RequestedN != 3 {
			t.Fatalf("requested_n = %d, want 3", req.RequestedN)
		}
		if !req.WaitForResult {
			t.Fatalf("wait_for_result = %v, want true", req.WaitForResult)
		}
		return &mixedModeExecResult{
			TaskID:         taskID,
			AccountID:      77,
			ConversationID: "conv_n3_success",
			FileRefs:       []string{"file_a", "file_b", "file_c"},
			SignedURLs:     []string{"https://signed.example/0", "https://signed.example/1", "https://signed.example/2"},
			Images: []MixedModeImage{
				{URL: "/p/img/task_n3/0?exp=1&sig=a", FileID: "file_a", ContentType: "image/png", TaskID: taskID},
				{URL: "/p/img/task_n3/1?exp=1&sig=b", FileID: "file_b", ContentType: "image/png", TaskID: taskID},
				{URL: "/p/img/task_n3/2?exp=1&sig=c", FileID: "file_c", ContentType: "image/png", TaskID: taskID},
			},
		}, nil
	}
	h.Images = &ImagesHandler{
		Handler:          h,
		DAO:              taskStore,
		ImageAccResolver: stubImageAccountResolver{},
	}

	rec := &usage.Log{RequestID: "req_mixed_success_n3"}
	ak := &apikey.APIKey{ID: 2, UserID: 3, Enabled: true}
	c, _ := newJSONContext(t, "/v1/chat/completions", `{}`, ak)
	n := 3
	res, apiErr := h.executeMixedModeChatImage(c, rec, ak, "gpt-5-thinking", mixedModeRequestInput{
		Messages:       []chatgpt.ChatMessage{{Role: "user", Content: "生成3张连续故事图"}},
		RequestedN:     &n,
		ThinkingEffort: "standard",
	})
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if res == nil || len(res.Images) != 3 {
		t.Fatalf("images = %d, want 3", len(res.Images))
	}
	if taskStore.created[0].N != 3 {
		t.Fatalf("created n = %d, want 3", taskStore.created[0].N)
	}
	if len(bill.preDeducts) != 1 || bill.preDeducts[0].Amount != 900 {
		t.Fatalf("prededuct = %#v, want amount 900", bill.preDeducts)
	}
	if len(bill.settles) != 1 || bill.settles[0].Expected != 900 || bill.settles[0].Actual != 900 {
		t.Fatalf("settles = %#v", bill.settles)
	}
	if rec.ImageCount != 3 || rec.CreditCost != 900 {
		t.Fatalf("rec = %+v", *rec)
	}
}

func TestExecuteMixedModeChatImageExplicitAsyncLeavesUsagePending(t *testing.T) {
	chatModel := &modelpkg.Model{ID: 11, Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}
	imageModel := &modelpkg.Model{ID: 22, Slug: "gpt-image-2", Type: modelpkg.TypeImage, Enabled: true, ImagePricePerCall: 300}
	taskStore := &fakeImageTaskStore{}
	bill := &fakeBillingStore{}
	h := &Handler{
		Models: &fakeModelStore{
			bySlug:  map[string]*modelpkg.Model{"gpt-5-thinking": chatModel, "gpt-image-2": imageModel},
			enabled: []*modelpkg.Model{chatModel, imageModel},
		},
		Billing:  bill,
		Settings: fakeSettings{mixedEnabled: true},
	}
	h.mixedModeConversationRunner = func(_ context.Context, taskID string, _ *modelpkg.Model, req *mixedModePreparedRequest) (*mixedModeExecResult, *mixedModeAPIError) {
		if req.WaitForResult {
			t.Fatalf("wait_for_result = %v, want false", req.WaitForResult)
		}
		return &mixedModeExecResult{
			Status:         mixedModeExecStatusInProgress,
			TaskID:         taskID,
			AccountID:      77,
			ConversationID: "conv_pending",
			FileRefs:       []string{"file_partial_1"},
			Images: []MixedModeImage{
				{URL: "/p/img/task_pending/0?exp=1&sig=a", FileID: "file_partial_1", ContentType: "image/png", TaskID: taskID},
			},
			ImageTask: buildMixedModeImageTask(taskID, "conv_pending", req.RequestedN, 1, image.StatusRunning),
		}, nil
	}
	h.Images = &ImagesHandler{
		Handler:          h,
		DAO:              taskStore,
		ImageAccResolver: stubImageAccountResolver{},
	}

	rec := &usage.Log{RequestID: "req_mixed_pending"}
	ak := &apikey.APIKey{ID: 2, UserID: 3, Enabled: true}
	c, _ := newJSONContext(t, "/v1/chat/completions", `{}`, ak)
	n := 2
	waitForResult := false
	res, apiErr := h.executeMixedModeChatImage(c, rec, ak, "gpt-5-thinking", mixedModeRequestInput{
		Messages:       []chatgpt.ChatMessage{{Role: "user", Content: "生成2张连续故事图"}},
		RequestedN:     &n,
		WaitForResult:  &waitForResult,
		ThinkingEffort: "standard",
	})
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if res == nil || res.Status != mixedModeExecStatusInProgress {
		t.Fatalf("res = %+v, want in_progress", res)
	}
	if rec.Status != usage.StatusPending {
		t.Fatalf("rec.Status = %q, want pending", rec.Status)
	}
	if len(bill.settles) != 0 || len(bill.refunds) != 0 {
		t.Fatalf("billing should not finalize early, settles=%#v refunds=%#v", bill.settles, bill.refunds)
	}
	if taskStore.success.TaskID != "" {
		t.Fatalf("task should not be marked success early: %#v", taskStore.success)
	}
}

func TestExecuteMixedModeChatImageStrictFailureRefundsTask(t *testing.T) {
	chatModel := &modelpkg.Model{ID: 11, Slug: "gpt-5", Type: modelpkg.TypeChat, Enabled: true}
	imageModel := &modelpkg.Model{ID: 22, Slug: "gpt-image-2", Type: modelpkg.TypeImage, Enabled: true, ImagePricePerCall: 300}
	taskStore := &fakeImageTaskStore{}
	bill := &fakeBillingStore{}
	h := &Handler{
		Models: &fakeModelStore{
			bySlug:  map[string]*modelpkg.Model{"gpt-5": chatModel, "gpt-image-2": imageModel},
			enabled: []*modelpkg.Model{chatModel, imageModel},
		},
		Billing:  bill,
		Settings: fakeSettings{mixedEnabled: true},
	}
	h.mixedModeConversationRunner = func(context.Context, string, *modelpkg.Model, *mixedModePreparedRequest) (*mixedModeExecResult, *mixedModeAPIError) {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_image_not_returned",
			Message: "上游本轮对话未返回图片结果,没有自动降级到旧图片接口",
		}
	}
	h.Images = &ImagesHandler{
		Handler:          h,
		DAO:              taskStore,
		ImageAccResolver: stubImageAccountResolver{},
	}

	rec := &usage.Log{RequestID: "req_mixed_failure"}
	ak := &apikey.APIKey{ID: 2, UserID: 3, Enabled: true}
	c, _ := newJSONContext(t, "/v1/chat/completions", `{}`, ak)
	_, apiErr := h.executeMixedModeChatImage(c, rec, ak, "gpt-5", mixedModeRequestInput{
		Messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
	})
	if apiErr == nil || apiErr.Code != "upstream_image_not_returned" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if len(taskStore.failed) != 1 || taskStore.failed[0].Code != "upstream_image_not_returned" {
		t.Fatalf("failed tasks = %#v", taskStore.failed)
	}
	if len(bill.refunds) != 1 || bill.refunds[0].Amount != 300 {
		t.Fatalf("refunds = %#v, want amount 300", bill.refunds)
	}
	if len(bill.settles) != 0 {
		t.Fatalf("settles = %#v, want none", bill.settles)
	}
}

func TestExecuteMixedModeChatImageZeroImagesRefundsRequestedCount(t *testing.T) {
	chatModel := &modelpkg.Model{ID: 11, Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}
	imageModel := &modelpkg.Model{ID: 22, Slug: "gpt-image-2", Type: modelpkg.TypeImage, Enabled: true, ImagePricePerCall: 300}
	taskStore := &fakeImageTaskStore{}
	bill := &fakeBillingStore{}
	h := &Handler{
		Models: &fakeModelStore{
			bySlug:  map[string]*modelpkg.Model{"gpt-5-thinking": chatModel, "gpt-image-2": imageModel},
			enabled: []*modelpkg.Model{chatModel, imageModel},
		},
		Billing:  bill,
		Settings: fakeSettings{mixedEnabled: true},
	}
	h.mixedModeConversationRunner = func(context.Context, string, *modelpkg.Model, *mixedModePreparedRequest) (*mixedModeExecResult, *mixedModeAPIError) {
		return nil, &mixedModeAPIError{
			Status:  http.StatusBadGateway,
			Code:    "upstream_image_not_returned",
			Message: "上游会话在规定时间内没有返回图片结果,请稍后重试",
		}
	}
	h.Images = &ImagesHandler{
		Handler:          h,
		DAO:              taskStore,
		ImageAccResolver: stubImageAccountResolver{},
	}

	rec := &usage.Log{RequestID: "req_mixed_zero_images"}
	ak := &apikey.APIKey{ID: 2, UserID: 3, Enabled: true}
	c, _ := newJSONContext(t, "/v1/chat/completions", `{}`, ak)
	n := 3
	_, apiErr := h.executeMixedModeChatImage(c, rec, ak, "gpt-5-thinking", mixedModeRequestInput{
		Messages:       []chatgpt.ChatMessage{{Role: "user", Content: "生成3张连续故事图"}},
		RequestedN:     &n,
		ThinkingEffort: "standard",
	})
	if apiErr == nil || apiErr.Code != "upstream_image_not_returned" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if len(taskStore.failed) != 1 || taskStore.failed[0].Code != "upstream_image_not_returned" {
		t.Fatalf("failed tasks = %#v", taskStore.failed)
	}
	if len(bill.refunds) != 1 || bill.refunds[0].Amount != 900 {
		t.Fatalf("refunds = %#v, want amount 900", bill.refunds)
	}
	if len(bill.settles) != 0 {
		t.Fatalf("settles = %#v, want none", bill.settles)
	}
}

func TestExecuteMixedModeChatImagePartialSuccessSettlesActualCount(t *testing.T) {
	chatModel := &modelpkg.Model{ID: 11, Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}
	imageModel := &modelpkg.Model{ID: 22, Slug: "gpt-image-2", Type: modelpkg.TypeImage, Enabled: true, ImagePricePerCall: 300}
	taskStore := &fakeImageTaskStore{}
	bill := &fakeBillingStore{}
	h := &Handler{
		Models: &fakeModelStore{
			bySlug:  map[string]*modelpkg.Model{"gpt-5-thinking": chatModel, "gpt-image-2": imageModel},
			enabled: []*modelpkg.Model{chatModel, imageModel},
		},
		Billing:  bill,
		Settings: fakeSettings{mixedEnabled: true},
	}
	h.mixedModeConversationRunner = func(_ context.Context, _ string, _ *modelpkg.Model, req *mixedModePreparedRequest) (*mixedModeExecResult, *mixedModeAPIError) {
		if !req.WaitForResult {
			t.Fatalf("wait_for_result = %v, want true", req.WaitForResult)
		}
		return &mixedModeExecResult{
			AccountID:      99,
			ConversationID: "conv_partial",
			FileRefs:       []string{"file_1", "file_2"},
			SignedURLs:     []string{"https://signed.example/1", "https://signed.example/2"},
			Images: []MixedModeImage{
				{URL: "/p/img/task_partial/0?exp=1&sig=a", FileID: "file_1", ContentType: "image/png", TaskID: "task_partial"},
				{URL: "/p/img/task_partial/1?exp=1&sig=b", FileID: "file_2", ContentType: "image/png", TaskID: "task_partial"},
			},
		}, nil
	}
	h.Images = &ImagesHandler{
		Handler:          h,
		DAO:              taskStore,
		ImageAccResolver: stubImageAccountResolver{},
	}

	rec := &usage.Log{RequestID: "req_mixed_partial"}
	ak := &apikey.APIKey{ID: 2, UserID: 3, Enabled: true}
	c, _ := newJSONContext(t, "/v1/chat/completions", `{}`, ak)
	n := 3
	res, apiErr := h.executeMixedModeChatImage(c, rec, ak, "gpt-5-thinking", mixedModeRequestInput{
		Messages:       []chatgpt.ChatMessage{{Role: "user", Content: "生成3张连续故事图"}},
		RequestedN:     &n,
		ThinkingEffort: "standard",
	})
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if res == nil || len(res.Images) != 2 {
		t.Fatalf("images = %d, want 2", len(res.Images))
	}
	if res.ImageTask != nil {
		t.Fatalf("image_task = %#v, want nil", res.ImageTask)
	}
	if len(bill.preDeducts) != 1 || bill.preDeducts[0].Amount != 900 {
		t.Fatalf("prededuct = %#v, want amount 900", bill.preDeducts)
	}
	if len(bill.settles) != 1 || bill.settles[0].Expected != 900 || bill.settles[0].Actual != 600 {
		t.Fatalf("settles = %#v", bill.settles)
	}
	if rec.ImageCount != 2 || rec.CreditCost != 600 {
		t.Fatalf("rec = %+v", *rec)
	}
}

func TestExecuteMixedModeChatImageValidationAndGuardrails(t *testing.T) {
	chatModel := &modelpkg.Model{ID: 11, Slug: "gpt-5", Type: modelpkg.TypeChat, Enabled: true}
	imageModel := &modelpkg.Model{ID: 22, Slug: "gpt-image-2", Type: modelpkg.TypeImage, Enabled: true, ImagePricePerCall: 300}
	imageOnlyModel := &modelpkg.Model{ID: 33, Slug: "gpt-image-only", Type: modelpkg.TypeImage, Enabled: true}

	makeHandler := func() *Handler {
		h := &Handler{
			Models: &fakeModelStore{
				bySlug: map[string]*modelpkg.Model{
					"gpt-5":          chatModel,
					"gpt-image-2":    imageModel,
					"gpt-image-only": imageOnlyModel,
				},
				enabled: []*modelpkg.Model{chatModel, imageModel, imageOnlyModel},
			},
			Billing:  &fakeBillingStore{},
			Settings: fakeSettings{mixedEnabled: true},
		}
		h.Images = &ImagesHandler{
			Handler:          h,
			DAO:              &fakeImageTaskStore{},
			ImageAccResolver: stubImageAccountResolver{},
		}
		return h
	}

	cases := []struct {
		name     string
		handler  func() *Handler
		ak       *apikey.APIKey
		model    string
		input    mixedModeRequestInput
		wantCode string
	}{
		{
			name: "feature_disabled",
			handler: func() *Handler {
				h := makeHandler()
				h.Settings = fakeSettings{mixedEnabled: false}
				return h
			},
			ak:       &apikey.APIKey{ID: 1, UserID: 2},
			model:    "gpt-5",
			input:    mixedModeRequestInput{Messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}}},
			wantCode: "feature_disabled",
		},
		{
			name:     "missing_prompt",
			handler:  makeHandler,
			ak:       &apikey.APIKey{ID: 1, UserID: 2},
			model:    "gpt-5",
			input:    mixedModeRequestInput{Messages: []chatgpt.ChatMessage{{Role: "assistant", Content: "没有 user"}}},
			wantCode: "invalid_request_error",
		},
		{
			name:    "model_not_allowed",
			handler: makeHandler,
			ak: &apikey.APIKey{
				ID:            1,
				UserID:        2,
				AllowedModels: sql.NullString{String: `["gpt-4o"]`, Valid: true},
			},
			model:    "gpt-5",
			input:    mixedModeRequestInput{Messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}}},
			wantCode: "model_not_allowed",
		},
		{
			name:     "model_type_mismatch",
			handler:  makeHandler,
			ak:       &apikey.APIKey{ID: 1, UserID: 2},
			model:    "gpt-image-only",
			input:    mixedModeRequestInput{Messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}}},
			wantCode: "model_type_mismatch",
		},
		{
			name: "insufficient_balance",
			handler: func() *Handler {
				h := makeHandler()
				h.Billing = &fakeBillingStore{preDeductErr: billing.ErrInsufficient}
				return h
			},
			ak:       &apikey.APIKey{ID: 1, UserID: 2},
			model:    "gpt-5",
			input:    mixedModeRequestInput{Messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}}},
			wantCode: "insufficient_balance",
		},
		{
			name: "rate_limit_rpm",
			handler: func() *Handler {
				h := makeHandler()
				h.Limiter = fakeLimiterStore{rpmAllowed: false, tpmAllowed: true}
				return h
			},
			ak:       &apikey.APIKey{ID: 1, UserID: 2, RPM: 5},
			model:    "gpt-5",
			input:    mixedModeRequestInput{Messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}}},
			wantCode: "rate_limit_rpm",
		},
		{
			name:    "thinking_effort_non_thinking_model",
			handler: makeHandler,
			ak:      &apikey.APIKey{ID: 1, UserID: 2},
			model:   "gpt-5",
			input: mixedModeRequestInput{
				Messages:       []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
				ThinkingEffort: "standard",
			},
			wantCode: "invalid_request_error",
		},
		{
			name:    "invalid_n",
			handler: makeHandler,
			ak:      &apikey.APIKey{ID: 1, UserID: 2},
			model:   "gpt-5",
			input: func() mixedModeRequestInput {
				n := 0
				return mixedModeRequestInput{
					Messages:   []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
					RequestedN: &n,
				}
			}(),
			wantCode: "invalid_request_error",
		},
		{
			name:    "n_over_default_cap",
			handler: makeHandler,
			ak:      &apikey.APIKey{ID: 1, UserID: 2},
			model:   "gpt-5",
			input: func() mixedModeRequestInput {
				n := 11
				return mixedModeRequestInput{
					Messages:   []chatgpt.ChatMessage{{Role: "user", Content: "生成11张连续故事图"}},
					RequestedN: &n,
				}
			}(),
			wantCode: "invalid_request_error",
		},
		{
			name: "n_over_runtime_cap",
			handler: func() *Handler {
				h := makeHandler()
				h.Settings = fakeSettings{mixedEnabled: true, maxN: 2}
				return h
			},
			ak:    &apikey.APIKey{ID: 1, UserID: 2},
			model: "gpt-5",
			input: func() mixedModeRequestInput {
				n := 3
				return mixedModeRequestInput{
					Messages:   []chatgpt.ChatMessage{{Role: "user", Content: "画三张连续故事图"}},
					RequestedN: &n,
				}
			}(),
			wantCode: "invalid_request_error",
		},
		{
			name: "prompt_count_conflict",
			handler: func() *Handler {
				h := makeHandler()
				h.Models = &fakeModelStore{
					bySlug: map[string]*modelpkg.Model{
						"gpt-5-thinking": {ID: 44, Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true},
						"gpt-image-2":    imageModel,
					},
					enabled: []*modelpkg.Model{
						{ID: 44, Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true},
						imageModel,
					},
				}
				return h
			},
			ak:    &apikey.APIKey{ID: 1, UserID: 2},
			model: "gpt-5-thinking",
			input: func() mixedModeRequestInput {
				n := 2
				return mixedModeRequestInput{
					Messages:   []chatgpt.ChatMessage{{Role: "user", Content: "生成3张连续故事图"}},
					RequestedN: &n,
				}
			}(),
			wantCode: "invalid_request_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.handler()
			rec := &usage.Log{RequestID: "req_" + tc.name}
			c, _ := newJSONContext(t, "/v1/chat/completions", `{}`, tc.ak)
			_, apiErr := h.executeMixedModeChatImage(c, rec, tc.ak, tc.model, tc.input)
			if apiErr == nil || apiErr.Code != tc.wantCode {
				t.Fatalf("apiErr = %+v, want code %s", apiErr, tc.wantCode)
			}
		})
	}
}

func TestExecuteMixedModeChatImageLogsStructuredFields(t *testing.T) {
	logFile, err := os.CreateTemp("", "gpt2api-mixed-mode-*.log")
	if err != nil {
		t.Fatalf("create temp log file: %v", err)
	}
	logPath := logFile.Name()
	if err := logFile.Close(); err != nil {
		t.Fatalf("close temp log file: %v", err)
	}
	if err := logger.Init("info", "json", logPath); err != nil {
		t.Fatalf("init logger: %v", err)
	}
	t.Cleanup(logger.Sync)

	chatModel := &modelpkg.Model{ID: 11, Slug: "gpt-5-thinking", Type: modelpkg.TypeChat, Enabled: true}
	imageModel := &modelpkg.Model{ID: 22, Slug: "gpt-image-2", Type: modelpkg.TypeImage, Enabled: true, ImagePricePerCall: 300}
	taskStore := &fakeImageTaskStore{}
	bill := &fakeBillingStore{}
	h := &Handler{
		Models: &fakeModelStore{
			bySlug:  map[string]*modelpkg.Model{"gpt-5-thinking": chatModel, "gpt-image-2": imageModel},
			enabled: []*modelpkg.Model{chatModel, imageModel},
		},
		Billing:  bill,
		Settings: fakeSettings{mixedEnabled: true},
	}
	callCount := 0
	h.mixedModeConversationRunner = func(_ context.Context, taskID string, _ *modelpkg.Model, req *mixedModePreparedRequest) (*mixedModeExecResult, *mixedModeAPIError) {
		callCount++
		if req.RequestedN != 3 {
			t.Fatalf("requested_n = %d, want 3", req.RequestedN)
		}
		if callCount == 1 {
			return &mixedModeExecResult{
				TaskID:         taskID,
				AccountID:      77,
				ConversationID: "conv_logs_full",
				ReasoningText:  "先规划连续故事分镜。",
				FileRefs:       []string{"file_a", "file_b", "file_c"},
				SignedURLs:     []string{"https://signed.example/0", "https://signed.example/1", "https://signed.example/2"},
				Images: []MixedModeImage{
					{URL: "/p/img/task_logs_full/0?exp=1&sig=a", FileID: "file_a", ContentType: "image/png", TaskID: taskID},
					{URL: "/p/img/task_logs_full/1?exp=1&sig=b", FileID: "file_b", ContentType: "image/png", TaskID: taskID},
					{URL: "/p/img/task_logs_full/2?exp=1&sig=c", FileID: "file_c", ContentType: "image/png", TaskID: taskID},
				},
			}, nil
		}
		return &mixedModeExecResult{
			TaskID:         taskID,
			AccountID:      77,
			ConversationID: "conv_logs_partial",
			ReasoningText:  "先规划连续故事分镜。",
			FileRefs:       []string{"file_d", "file_e"},
			SignedURLs:     []string{"https://signed.example/3", "https://signed.example/4"},
			Images: []MixedModeImage{
				{URL: "/p/img/task_logs_partial/0?exp=1&sig=d", FileID: "file_d", ContentType: "image/png", TaskID: taskID},
				{URL: "/p/img/task_logs_partial/1?exp=1&sig=e", FileID: "file_e", ContentType: "image/png", TaskID: taskID},
			},
		}, nil
	}
	h.Images = &ImagesHandler{
		Handler:          h,
		DAO:              taskStore,
		ImageAccResolver: stubImageAccountResolver{},
	}

	ak := &apikey.APIKey{ID: 2, UserID: 3, Enabled: true}
	n := 3
	c1, _ := newJSONContext(t, "/v1/chat/completions", `{}`, ak)
	if _, apiErr := h.executeMixedModeChatImage(c1, &usage.Log{RequestID: "req_logs_full"}, ak, "gpt-5-thinking", mixedModeRequestInput{
		Messages:       []chatgpt.ChatMessage{{Role: "user", Content: "生成3张连续故事图"}},
		RequestedN:     &n,
		ThinkingEffort: "standard",
	}); apiErr != nil {
		t.Fatalf("full apiErr = %+v", apiErr)
	}
	c2, _ := newJSONContext(t, "/v1/chat/completions", `{}`, ak)
	if _, apiErr := h.executeMixedModeChatImage(c2, &usage.Log{RequestID: "req_logs_partial"}, ak, "gpt-5-thinking", mixedModeRequestInput{
		Messages:       []chatgpt.ChatMessage{{Role: "user", Content: "生成3张连续故事图"}},
		RequestedN:     &n,
		ThinkingEffort: "standard",
	}); apiErr != nil {
		t.Fatalf("partial apiErr = %+v", apiErr)
	}
	logger.Sync()

	entries := readJSONLogEntries(t, logPath)

	start := findJSONLogEntry(t, entries, "req_logs_full", "chat mixed image start")
	assertJSONLogInt(t, start, "requested_n", 3)
	assertJSONLogInt(t, start, "actual_n", 0)
	assertJSONLogString(t, start, "strategy", "picture_v2_thinking")
	assertJSONLogString(t, start, "thinking_effort", "standard")
	assertJSONLogString(t, start, "conversation_id", "")
	assertJSONLogBool(t, start, "partial_success", false)

	success := findJSONLogEntry(t, entries, "req_logs_full", "chat mixed image success")
	assertJSONLogInt(t, success, "requested_n", 3)
	assertJSONLogInt(t, success, "actual_n", 3)
	assertJSONLogString(t, success, "strategy", "picture_v2_thinking")
	assertJSONLogString(t, success, "thinking_effort", "standard")
	assertJSONLogString(t, success, "conversation_id", "conv_logs_full")
	assertJSONLogBool(t, success, "partial_success", false)
	assertJSONLogBool(t, success, "thinking_triggered", true)
	assertJSONLogInt(t, success, "reasoning_len", len("先规划连续故事分镜。"))

	partial := findJSONLogEntry(t, entries, "req_logs_partial", "chat mixed image partial success")
	assertJSONLogInt(t, partial, "requested_n", 3)
	assertJSONLogInt(t, partial, "actual_n", 2)
	assertJSONLogString(t, partial, "strategy", "picture_v2_thinking")
	assertJSONLogString(t, partial, "thinking_effort", "standard")
	assertJSONLogString(t, partial, "conversation_id", "conv_logs_partial")
	assertJSONLogBool(t, partial, "partial_success", true)
	assertJSONLogBool(t, partial, "thinking_triggered", true)
	assertJSONLogInt(t, partial, "reasoning_len", len("先规划连续故事分镜。"))

	partialSuccess := findJSONLogEntry(t, entries, "req_logs_partial", "chat mixed image success")
	assertJSONLogInt(t, partialSuccess, "requested_n", 3)
	assertJSONLogInt(t, partialSuccess, "actual_n", 2)
	assertJSONLogString(t, partialSuccess, "conversation_id", "conv_logs_partial")
	assertJSONLogBool(t, partialSuccess, "partial_success", true)
}

func readJSONLogEntries(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := bytesToJSONLines(buf)
	entries := make([]map[string]interface{}, 0, len(lines))
	for _, line := range lines {
		var entry map[string]interface{}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("decode log line %q: %v", string(line), err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func bytesToJSONLines(buf []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range buf {
		if b != '\n' {
			continue
		}
		if i > start {
			lines = append(lines, append([]byte(nil), buf[start:i]...))
		}
		start = i + 1
	}
	if start < len(buf) {
		lines = append(lines, append([]byte(nil), buf[start:]...))
	}
	return lines
}

func findJSONLogEntry(t *testing.T, entries []map[string]interface{}, requestID, msg string) map[string]interface{} {
	t.Helper()
	for _, entry := range entries {
		if entry["msg"] == msg && entry["request_id"] == requestID {
			return entry
		}
	}
	t.Fatalf("missing log entry request_id=%s msg=%s", requestID, msg)
	return nil
}

func assertJSONLogString(t *testing.T, entry map[string]interface{}, key, want string) {
	t.Helper()
	got, ok := entry[key].(string)
	if !ok || got != want {
		t.Fatalf("log[%q] = %#v, want %q", key, entry[key], want)
	}
}

func assertJSONLogInt(t *testing.T, entry map[string]interface{}, key string, want int) {
	t.Helper()
	got, ok := entry[key].(float64)
	if !ok || int(got) != want {
		t.Fatalf("log[%q] = %#v, want %d", key, entry[key], want)
	}
}

func assertJSONLogBool(t *testing.T, entry map[string]interface{}, key string, want bool) {
	t.Helper()
	got, ok := entry[key].(bool)
	if !ok || got != want {
		t.Fatalf("log[%q] = %#v, want %t", key, entry[key], want)
	}
}
