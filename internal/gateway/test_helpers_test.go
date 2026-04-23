package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/apikey"
	"github.com/432539/gpt2api/internal/image"
	modelpkg "github.com/432539/gpt2api/internal/model"
	"github.com/432539/gpt2api/internal/usage"
)

type fakeSettings struct {
	mixedEnabled            bool
	defaultWaitForResult    bool
	defaultWaitForResultSet bool
	upstreamSec             int
	sseSec                  int
	runSec                  int
	pollSec                 int
	thinkingRunSec          int
	thinkingPollSec         int
	thinkingStrategy        string
	maxN                    int
}

func (s fakeSettings) GatewayUpstreamTimeoutSec() int { return s.upstreamSec }
func (s fakeSettings) GatewaySSEReadTimeoutSec() int  { return s.sseSec }
func (s fakeSettings) GatewayChatImageDefaultWaitForResult() bool {
	if !s.defaultWaitForResultSet {
		return true
	}
	return s.defaultWaitForResult
}
func (s fakeSettings) GatewayChatImageRunTimeoutSec() int {
	return s.runSec
}
func (s fakeSettings) GatewayChatImagePollMaxWaitSec() int {
	return s.pollSec
}
func (s fakeSettings) GatewayChatImageThinkingRunTimeoutSec() int {
	return s.thinkingRunSec
}
func (s fakeSettings) GatewayChatImageThinkingPollMaxWaitSec() int {
	return s.thinkingPollSec
}
func (s fakeSettings) GatewayChatImageMixedEnabled() bool {
	return s.mixedEnabled
}
func (s fakeSettings) GatewayChatImageThinkingStrategy() string {
	if s.thinkingStrategy == "" {
		return "picture_v2_thinking"
	}
	return s.thinkingStrategy
}
func (s fakeSettings) GatewayChatImageMaxN() int {
	if s.maxN <= 0 {
		return 10
	}
	return s.maxN
}

type fakeModelStore struct {
	bySlug  map[string]*modelpkg.Model
	enabled []*modelpkg.Model
}

func (s *fakeModelStore) BySlug(_ context.Context, slug string) (*modelpkg.Model, error) {
	return s.bySlug[slug], nil
}

func (s *fakeModelStore) ListEnabled(context.Context) ([]*modelpkg.Model, error) {
	return s.enabled, nil
}

type billingCall struct {
	UserID uint64
	KeyID  uint64
	Amount int64
	RefID  string
	Remark string
}

type settleCall struct {
	UserID   uint64
	KeyID    uint64
	Expected int64
	Actual   int64
	RefID    string
	Remark   string
}

type fakeBillingStore struct {
	preDeductErr error
	preDeducts   []billingCall
	settles      []settleCall
	refunds      []billingCall
}

func (s *fakeBillingStore) PreDeduct(_ context.Context, userID, keyID uint64, amount int64, refID, remark string) error {
	s.preDeducts = append(s.preDeducts, billingCall{UserID: userID, KeyID: keyID, Amount: amount, RefID: refID, Remark: remark})
	return s.preDeductErr
}

func (s *fakeBillingStore) Settle(_ context.Context, userID, keyID uint64, expected, actual int64, refID, remark string) error {
	s.settles = append(s.settles, settleCall{UserID: userID, KeyID: keyID, Expected: expected, Actual: actual, RefID: refID, Remark: remark})
	return nil
}

func (s *fakeBillingStore) Refund(_ context.Context, userID, keyID uint64, expected int64, refID, remark string) error {
	s.refunds = append(s.refunds, billingCall{UserID: userID, KeyID: keyID, Amount: expected, RefID: refID, Remark: remark})
	return nil
}

type fakeLimiterStore struct {
	rpmAllowed bool
	tpmAllowed bool
}

func (s fakeLimiterStore) AllowRPM(context.Context, uint64, int) (bool, float64, error) {
	return s.rpmAllowed, 0, nil
}

func (s fakeLimiterStore) AllowTPM(context.Context, uint64, int64, int64) (bool, float64, error) {
	return s.tpmAllowed, 0, nil
}

func (fakeLimiterStore) AdjustTPM(context.Context, uint64, int64, int64) {}

type fakeKeyDAO struct {
	touches []struct {
		KeyID uint64
		IP    string
		Cost  int64
	}
}

func (d *fakeKeyDAO) TouchUsage(_ context.Context, id uint64, lastIP string, deltaQuota int64) error {
	d.touches = append(d.touches, struct {
		KeyID uint64
		IP    string
		Cost  int64
	}{KeyID: id, IP: lastIP, Cost: deltaQuota})
	return nil
}

type fakeKeyStore struct{ dao *fakeKeyDAO }

func (s fakeKeyStore) TouchUsage(ctx context.Context, id uint64, lastIP string, deltaQuota int64) error {
	if s.dao == nil {
		return nil
	}
	return s.dao.TouchUsage(ctx, id, lastIP, deltaQuota)
}

type fakeUsageStore struct {
	rows      []usage.Log
	finalized []struct {
		RequestID string
		Patch     usage.FinalizePatch
	}
}

func (s *fakeUsageStore) Write(row *usage.Log) {
	if row == nil {
		return
	}
	cp := *row
	s.rows = append(s.rows, cp)
}

func (s *fakeUsageStore) Finalize(_ context.Context, requestID string, patch usage.FinalizePatch) error {
	s.finalized = append(s.finalized, struct {
		RequestID string
		Patch     usage.FinalizePatch
	}{RequestID: requestID, Patch: patch})
	for i := range s.rows {
		if s.rows[i].RequestID != requestID {
			continue
		}
		s.rows[i].AccountID = patch.AccountID
		s.rows[i].ImageCount = patch.ImageCount
		s.rows[i].CreditCost = patch.CreditCost
		s.rows[i].DurationMs = patch.DurationMs
		s.rows[i].Status = patch.Status
		s.rows[i].ErrorCode = patch.ErrorCode
		return nil
	}
	return nil
}

type fakeImageTaskStore struct {
	created []*image.Task
	running []struct {
		TaskID    string
		AccountID uint64
		ConvID    string
	}
	progress []struct {
		TaskID     string
		ConvID     string
		FileIDs    []string
		ResultURLs []string
	}
	failed     []struct{ TaskID, Code string }
	setAccount []struct {
		TaskID    string
		AccountID uint64
	}
	success struct {
		TaskID     string
		ConvID     string
		FileIDs    []string
		ResultURLs []string
		CreditCost int64
	}
	updateCosts []struct {
		TaskID string
		Cost   int64
	}
	tasks map[string]*image.Task
}

func (s *fakeImageTaskStore) Create(_ context.Context, t *image.Task) error {
	cp := *t
	s.created = append(s.created, &cp)
	if s.tasks == nil {
		s.tasks = map[string]*image.Task{}
	}
	s.tasks[t.TaskID] = &cp
	return nil
}

func (s *fakeImageTaskStore) MarkRunning(_ context.Context, taskID string, accountID uint64, convID string) error {
	s.running = append(s.running, struct {
		TaskID    string
		AccountID uint64
		ConvID    string
	}{TaskID: taskID, AccountID: accountID, ConvID: convID})
	if s.tasks != nil && s.tasks[taskID] != nil {
		s.tasks[taskID].Status = image.StatusRunning
		s.tasks[taskID].AccountID = accountID
		if convID != "" {
			s.tasks[taskID].ConversationID = convID
		}
	}
	return nil
}

func (s *fakeImageTaskStore) SetAccount(_ context.Context, taskID string, accountID uint64) error {
	s.setAccount = append(s.setAccount, struct {
		TaskID    string
		AccountID uint64
	}{TaskID: taskID, AccountID: accountID})
	if s.tasks != nil && s.tasks[taskID] != nil {
		s.tasks[taskID].AccountID = accountID
	}
	return nil
}

func (s *fakeImageTaskStore) UpdateProgress(_ context.Context, taskID, convID string, fileIDs, resultURLs []string) error {
	s.progress = append(s.progress, struct {
		TaskID     string
		ConvID     string
		FileIDs    []string
		ResultURLs []string
	}{
		TaskID:     taskID,
		ConvID:     convID,
		FileIDs:    append([]string(nil), fileIDs...),
		ResultURLs: append([]string(nil), resultURLs...),
	})
	if s.tasks != nil && s.tasks[taskID] != nil {
		s.tasks[taskID].Status = image.StatusRunning
		if convID != "" {
			s.tasks[taskID].ConversationID = convID
		}
		if fileIDs != nil {
			b, _ := json.Marshal(fileIDs)
			s.tasks[taskID].FileIDs = b
		}
		if resultURLs != nil {
			b, _ := json.Marshal(resultURLs)
			s.tasks[taskID].ResultURLs = b
		}
	}
	return nil
}

func (s *fakeImageTaskStore) MarkSuccess(_ context.Context, taskID, convID string, fileIDs, resultURLs []string, creditCost int64) error {
	s.success = struct {
		TaskID     string
		ConvID     string
		FileIDs    []string
		ResultURLs []string
		CreditCost int64
	}{
		TaskID: taskID, ConvID: convID, FileIDs: append([]string(nil), fileIDs...),
		ResultURLs: append([]string(nil), resultURLs...), CreditCost: creditCost,
	}
	if s.tasks != nil && s.tasks[taskID] != nil {
		s.tasks[taskID].Status = image.StatusSuccess
		s.tasks[taskID].ConversationID = convID
		s.tasks[taskID].CreditCost = creditCost
		fidB, _ := json.Marshal(fileIDs)
		urlB, _ := json.Marshal(resultURLs)
		s.tasks[taskID].FileIDs = fidB
		s.tasks[taskID].ResultURLs = urlB
	}
	return nil
}

func (s *fakeImageTaskStore) UpdateCost(_ context.Context, taskID string, cost int64) error {
	s.updateCosts = append(s.updateCosts, struct {
		TaskID string
		Cost   int64
	}{TaskID: taskID, Cost: cost})
	return nil
}

func (s *fakeImageTaskStore) MarkFailed(_ context.Context, taskID, errorCode string) error {
	s.failed = append(s.failed, struct{ TaskID, Code string }{TaskID: taskID, Code: errorCode})
	if s.tasks != nil && s.tasks[taskID] != nil {
		s.tasks[taskID].Status = image.StatusFailed
		s.tasks[taskID].Error = errorCode
	}
	return nil
}

func (s *fakeImageTaskStore) Get(_ context.Context, taskID string) (*image.Task, error) {
	if s.tasks == nil || s.tasks[taskID] == nil {
		return nil, image.ErrNotFound
	}
	cp := *s.tasks[taskID]
	return &cp, nil
}

type stubImageAccountResolver struct{}

func (stubImageAccountResolver) AuthToken(context.Context, uint64) (string, string, string, error) {
	return "", "", "", nil
}

func (stubImageAccountResolver) ProxyURL(context.Context, uint64) string { return "" }

func newJSONContext(t *testing.T, path string, body string, ak *apikey.APIKey) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:1234"
	c.Request = req
	if ak != nil {
		c.Set(apikey.CtxKey, ak)
		c.Set(apikey.CtxKeyOwner, ak.UserID)
	}
	return c, w
}

func withFrozenResponseMeta(t *testing.T, at time.Time, ids ...string) {
	t.Helper()
	oldNow := nowFunc
	oldUUID := newUUIDFunc
	idx := 0
	nowFunc = func() time.Time { return at }
	newUUIDFunc = func() string {
		if idx >= len(ids) {
			return "fixture-id"
		}
		v := ids[idx]
		idx++
		return v
	}
	t.Cleanup(func() {
		nowFunc = oldNow
		newUUIDFunc = oldUUID
	})
}

func withFrozenImageTaskIDs(t *testing.T, ids ...string) {
	t.Helper()
	oldTaskID := imageTaskIDFunc
	idx := 0
	imageTaskIDFunc = func() string {
		if idx >= len(ids) {
			return "img_fixture_task"
		}
		v := ids[idx]
		idx++
		return v
	}
	t.Cleanup(func() {
		imageTaskIDFunc = oldTaskID
	})
}

func assertJSONGolden(t *testing.T, got []byte, name string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	var gotJSON interface{}
	var wantJSON interface{}
	if err := json.Unmarshal(got, &gotJSON); err != nil {
		t.Fatalf("decode got json: %v\n%s", err, string(got))
	}
	if err := json.Unmarshal(want, &wantJSON); err != nil {
		t.Fatalf("decode golden json: %v\n%s", err, string(want))
	}
	if reflect.DeepEqual(gotJSON, wantJSON) {
		return
	}
	gotPretty, _ := json.MarshalIndent(gotJSON, "", "  ")
	wantPretty, _ := json.MarshalIndent(wantJSON, "", "  ")
	t.Fatalf("json mismatch for %s\nwant:\n%s\n\ngot:\n%s", name, string(wantPretty), string(gotPretty))
}

func assertTextGolden(t *testing.T, got string, name string) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	got = strings.TrimRight(got, "\r\n")
	wantText := strings.TrimRight(string(want), "\r\n")
	if got == wantText {
		return
	}
	t.Fatalf("text mismatch for %s\nwant:\n%s\n\ngot:\n%s", name, wantText, got)
}
