package account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// QuotaSettings 热更新参数。
type QuotaSettings interface {
	AccountQuotaProbeEnabled() bool
	AccountQuotaProbeIntervalSec() int
	AccountRefreshConcurrency() int // 复用刷新并发上限
}

// QuotaResult 探测结果。
type QuotaResult struct {
	AccountID       uint64    `json:"account_id"`
	Email           string    `json:"email"`
	OK              bool      `json:"ok"`
	Remaining       int       `json:"remaining"`
	Total           int       `json:"total"`
	ResetAt         time.Time `json:"reset_at,omitempty"`
	DefaultModel    string    `json:"default_model,omitempty"`    // 如 gpt-5-3
	BlockedFeatures []string  `json:"blocked_features,omitempty"` // 被风控限制的功能列表
	Error           string    `json:"error,omitempty"`
}

// QuotaProber 后台定期探测账号图片剩余额度。
type QuotaProber struct {
	svc      *Service
	settings QuotaSettings
	log      *zap.Logger
	client   *http.Client

	proxyResolver AccountProxyResolver

	kick chan struct{}
}

func NewQuotaProber(svc *Service, settings QuotaSettings, logger *zap.Logger) *QuotaProber {
	return &QuotaProber{
		svc:      svc,
		settings: settings,
		log:      logger,
		client: &http.Client{
			Timeout: 20 * time.Second,
		},
		kick: make(chan struct{}, 1),
	}
}

// SetProxyResolver 注入账号代理解析器;未注入则直连。
func (q *QuotaProber) SetProxyResolver(pr AccountProxyResolver) { q.proxyResolver = pr }

// clientFor 参见 Refresher.clientFor 的语义。
func (q *QuotaProber) clientFor(ctx context.Context, accountID uint64) *http.Client {
	if q.proxyResolver == nil {
		return q.client
	}
	pu := q.proxyResolver.ProxyURLForAccount(ctx, accountID)
	if pu == "" {
		return q.client
	}
	u, err := url.Parse(pu)
	if err != nil {
		q.log.Warn("invalid proxy url for quota probe, fallback direct",
			zap.Uint64("account_id", accountID), zap.Error(err))
		return q.client
	}
	tr := &http.Transport{
		Proxy:               http.ProxyURL(u),
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        16,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{Transport: tr, Timeout: q.client.Timeout}
}

func (q *QuotaProber) Kick() {
	select {
	case q.kick <- struct{}{}:
	default:
	}
}

// Run 后台循环。
func (q *QuotaProber) Run(ctx context.Context) {
	q.log.Info("account quota prober started")
	defer q.log.Info("account quota prober stopped")

	select {
	case <-ctx.Done():
		return
	case <-time.After(10 * time.Second):
	}

	for {
		// 最小扫描间隔 60s;实际复用探测最小间隔的 1/3 做节奏,至少 60s
		interval := time.Duration(q.settings.AccountQuotaProbeIntervalSec()/3) * time.Second
		if interval < 60*time.Second {
			interval = 60 * time.Second
		}

		if q.settings.AccountQuotaProbeEnabled() {
			q.scanOnce(ctx)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		case <-q.kick:
		}
	}
}

func (q *QuotaProber) scanOnce(ctx context.Context) {
	minInterval := q.settings.AccountQuotaProbeIntervalSec()
	conc := q.settings.AccountRefreshConcurrency()

	rows, err := q.svc.dao.ListNeedProbeQuota(ctx, minInterval, 256)
	if err != nil {
		q.log.Warn("list quota probe candidates failed", zap.Error(err))
		return
	}
	if len(rows) == 0 {
		return
	}

	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, a := range rows {
		a := a
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, _ = q.ProbeOne(ctx, a)
		}()
	}
	wg.Wait()
}

// ProbeByID 指定账号探测。
func (q *QuotaProber) ProbeByID(ctx context.Context, id uint64) (*QuotaResult, error) {
	a, err := q.svc.dao.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return q.ProbeOne(ctx, a)
}

// ProbeOne 执行一次探测。
// 访问 https://chatgpt.com/backend-api/rate_limits(需要 AT),挑选 image 相关条目汇总。
func (q *QuotaProber) ProbeOne(ctx context.Context, a *Account) (*QuotaResult, error) {
	res := &QuotaResult{AccountID: a.ID, Email: a.Email}
	at, err := q.svc.cipher.DecryptString(a.AuthTokenEnc)
	if err != nil || at == "" {
		res.Error = "AT 解密失败"
		_ = q.svc.dao.ApplyQuotaResult(ctx, a.ID, -1, -1, nil)
		return res, errors.New(res.Error)
	}

	probe, probeErr := q.doProbe(ctx, a.ID, at)
	if probeErr != nil {
		res.Error = friendlyProbeErr(probeErr)
		_ = q.svc.dao.ApplyQuotaResult(ctx, a.ID, -1, -1, nil)
		return res, probeErr
	}

	var resetPtr *time.Time
	if !probe.resetAt.IsZero() {
		resetPtr = &probe.resetAt
	}
	if err := q.svc.dao.ApplyQuotaResult(ctx, a.ID, probe.remaining, probe.total, resetPtr); err != nil {
		res.Error = "写库失败:" + err.Error()
		return res, err
	}
	res.OK = true
	res.Remaining = probe.remaining
	res.Total = probe.total
	res.ResetAt = probe.resetAt
	res.DefaultModel = probe.defaultModel
	res.BlockedFeatures = probe.blockedFeatures
	return res, nil
}

type probeOutcome struct {
	remaining       int
	total           int
	resetAt         time.Time
	defaultModel    string
	blockedFeatures []string
}

// doProbe 调 /backend-api/conversation/init。
//
// 这是 ChatGPT 网页左下角「今日还剩 XX 张图」的数据源,官方不会把这次调用计入额度消耗,
// 适合用于后台定时探测。
//
// 请求 body 参照抓包样例;响应关心的字段是:
//   - limits_progress[].feature_name == "image_gen" → remaining / reset_after
//   - default_model_slug  → 账号默认模型
//   - blocked_features    → 被风控限制的功能;非空需要关注
func (q *QuotaProber) doProbe(ctx context.Context, accountID uint64, accessToken string) (out probeOutcome, err error) {
	out.remaining = -1
	out.total = -1

	// timezone_offset_min: 跟 UI 一致发 -480(北京时间),非关键
	reqBody := []byte(`{"gizmo_id":null,"requested_default_model":null,"conversation_id":null,"timezone_offset_min":-480,"system_hints":["picture_v2"]}`)

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://chatgpt.com/backend-api/conversation/init", bytes.NewReader(reqBody))
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Referer", "https://chatgpt.com/")
	req.Header.Set("Origin", "https://chatgpt.com")
	req.Header.Set("User-Agent",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")

	resp, e := q.clientFor(ctx, accountID).Do(req)
	if e != nil {
		err = e
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		err = fmt.Errorf("conversation/init http=%d body=%s", resp.StatusCode, truncate(string(data), 200))
		return
	}

	var payload struct {
		Type             string   `json:"type"`
		BlockedFeatures  []string `json:"blocked_features"`
		DefaultModelSlug string   `json:"default_model_slug"`
		LimitsProgress   []struct {
			FeatureName string `json:"feature_name"`
			Remaining   *int   `json:"remaining"`
			ResetAfter  string `json:"reset_after"`
		} `json:"limits_progress"`
	}
	if err = json.Unmarshal(data, &payload); err != nil {
		return
	}
	out.defaultModel = payload.DefaultModelSlug
	out.blockedFeatures = payload.BlockedFeatures

	for _, item := range payload.LimitsProgress {
		if !isImageFeature(item.FeatureName) {
			continue
		}
		if item.Remaining != nil {
			if out.remaining < 0 || *item.Remaining < out.remaining {
				out.remaining = *item.Remaining
			}
		}
		if item.ResetAfter != "" {
			if t, e := time.Parse(time.RFC3339, item.ResetAfter); e == nil {
				if out.resetAt.IsZero() || t.Before(out.resetAt) {
					out.resetAt = t
				}
			}
		}
	}
	return
}

func isImageFeature(name string) bool {
	n := strings.ToLower(name)
	switch n {
	case "image_gen", "image_generation", "image_edit", "img_gen":
		return true
	}
	return strings.Contains(n, "image_gen") || strings.Contains(n, "img_gen")
}

func friendlyProbeErr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "http=401"):
		return "AT 已过期,无法探测额度"
	case strings.Contains(low, "http=403"):
		return "上游拒绝访问(403)"
	case strings.Contains(low, "http=429"):
		return "上游速率限制(429)"
	case strings.Contains(low, "timeout"), strings.Contains(low, "deadline exceeded"):
		return "探测超时"
	case strings.Contains(low, "connection refused"), strings.Contains(low, "no such host"):
		return "网络不通"
	default:
		if len(s) > 160 {
			s = s[:160] + "…"
		}
		return "探测失败:" + s
	}
}
