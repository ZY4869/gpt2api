package gateway

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/billing"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/upstream/chatgpt"
	"github.com/432539/gpt2api/internal/usage"
)

func TestExecuteMixedModeChatImageSuccessPersistsTaskAndSettlement(t *testing.T) {
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
	h.mixedModeConversationRunner = func(_ context.Context, taskID string, _ *modelpkg.Model, _ []chatgpt.ChatMessage) (*mixedModeExecResult, *mixedModeAPIError) {
		return &mixedModeExecResult{
			TaskID:         taskID,
			AccountID:      77,
			ConversationID: "conv_mixed_1",
			FileRefs:       []string{"file_a", "sed:sed_b"},
			SignedURLs:     []string{"https://signed.example/0", "https://signed.example/1"},
			Images: []MixedModeImage{
				{URL: "/p/img/task/0?exp=1&sig=a", FileID: "file_a", ContentType: "image/png", TaskID: taskID},
				{URL: "/p/img/task/1?exp=1&sig=b", FileID: "sed_b", ContentType: "image/png", TaskID: taskID},
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
	res, apiErr := h.executeMixedModeChatImage(c, rec, ak, "gpt-5", []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}})
	if apiErr != nil {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if res == nil || len(res.Images) != 2 {
		t.Fatalf("images = %d, want 2", len(res.Images))
	}
	if len(taskStore.created) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(taskStore.created))
	}
	if taskStore.created[0].ModelID != imageModel.ID {
		t.Fatalf("created model_id = %d, want %d", taskStore.created[0].ModelID, imageModel.ID)
	}
	if taskStore.success.ConvID != "conv_mixed_1" {
		t.Fatalf("conversation_id = %s, want conv_mixed_1", taskStore.success.ConvID)
	}
	if len(taskStore.success.FileIDs) != 2 || taskStore.success.FileIDs[1] != "sed:sed_b" {
		t.Fatalf("file ids = %#v", taskStore.success.FileIDs)
	}
	if len(bill.preDeducts) != 1 || bill.preDeducts[0].Amount != 1760 {
		t.Fatalf("prededuct = %#v, want amount 1760", bill.preDeducts)
	}
	if len(bill.settles) != 1 || bill.settles[0].Expected != 1760 || bill.settles[0].Actual != 880 {
		t.Fatalf("settles = %#v", bill.settles)
	}
	if len(bill.refunds) != 0 {
		t.Fatalf("refunds = %#v, want none", bill.refunds)
	}
	if len(keyDAO.touches) != 1 || keyDAO.touches[0].Cost != 880 {
		t.Fatalf("key touches = %#v", keyDAO.touches)
	}
	if rec.Status != usage.StatusSuccess || rec.Type != usage.TypeImage {
		t.Fatalf("rec status/type = %s/%s", rec.Status, rec.Type)
	}
	if rec.AccountID != 77 || rec.ImageCount != 2 || rec.CreditCost != 880 {
		t.Fatalf("rec usage fields = %+v", *rec)
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
	h.mixedModeConversationRunner = func(context.Context, string, *modelpkg.Model, []chatgpt.ChatMessage) (*mixedModeExecResult, *mixedModeAPIError) {
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
	_, apiErr := h.executeMixedModeChatImage(c, rec, ak, "gpt-5", []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}})
	if apiErr == nil || apiErr.Code != "upstream_image_not_returned" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if len(taskStore.failed) != 1 || taskStore.failed[0].Code != "upstream_image_not_returned" {
		t.Fatalf("failed tasks = %#v", taskStore.failed)
	}
	if len(bill.refunds) != 1 || bill.refunds[0].Amount != 1200 {
		t.Fatalf("refunds = %#v, want amount 1200", bill.refunds)
	}
	if len(bill.settles) != 0 {
		t.Fatalf("settles = %#v, want none", bill.settles)
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
		messages []chatgpt.ChatMessage
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
			messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
			wantCode: "feature_disabled",
		},
		{
			name:     "missing_prompt",
			handler:  makeHandler,
			ak:       &apikey.APIKey{ID: 1, UserID: 2},
			model:    "gpt-5",
			messages: []chatgpt.ChatMessage{{Role: "assistant", Content: "没有 user"}},
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
			messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
			wantCode: "model_not_allowed",
		},
		{
			name:     "model_type_mismatch",
			handler:  makeHandler,
			ak:       &apikey.APIKey{ID: 1, UserID: 2},
			model:    "gpt-image-only",
			messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
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
			messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
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
			messages: []chatgpt.ChatMessage{{Role: "user", Content: "画一只橘猫"}},
			wantCode: "rate_limit_rpm",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.handler()
			rec := &usage.Log{RequestID: "req_" + tc.name}
			c, _ := newJSONContext(t, "/v1/chat/completions", `{}`, tc.ak)
			_, apiErr := h.executeMixedModeChatImage(c, rec, tc.ak, tc.model, tc.messages)
			if apiErr == nil || apiErr.Code != tc.wantCode {
				t.Fatalf("apiErr = %+v, want code %s", apiErr, tc.wantCode)
			}
		})
	}
}
