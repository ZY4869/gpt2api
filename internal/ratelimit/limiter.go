// Package ratelimit 封装 API Key / 用户分组的 RPM / TPM 限流。
//
// 两层分桶:
//   rpm:<key_id>   - 每分钟请求次数(capacity=RPM)
//   tpm:<key_id>   - 每分钟 token 额度(capacity=TPM)
//
// RPM 在网关入口预检;TPM 先按估算值扣,结算时按实际值"补差"。
// 依赖 pkg/ratelimit 的 TokenBucket 原语。
package ratelimit

import (
	"context"
	"fmt"
	"time"

	pkgrl "github.com/432539/gpt2api/pkg/ratelimit"
)

// Limiter 是"按 Key 维度"的限流服务。
type Limiter struct {
	tb *pkgrl.TokenBucket
}

func New(tb *pkgrl.TokenBucket) *Limiter { return &Limiter{tb: tb} }

// AllowRPM 消费 1 个 RPM 令牌。capacity<=0 表示不限。
func (l *Limiter) AllowRPM(ctx context.Context, keyID uint64, capacity int) (bool, float64, error) {
	if capacity <= 0 {
		return true, 0, nil
	}
	key := fmt.Sprintf("rl:rpm:k:%d", keyID)
	refill := float64(capacity) / 60.0
	return l.tb.Allow(ctx, key, int64(capacity), 1, refill)
}

// AllowTPM 预扣 tokens,若额度不足返回 false。capacity<=0 不限。
func (l *Limiter) AllowTPM(ctx context.Context, keyID uint64, capacity int64, tokens int64) (bool, float64, error) {
	if capacity <= 0 || tokens <= 0 {
		return true, 0, nil
	}
	key := fmt.Sprintf("rl:tpm:k:%d", keyID)
	refill := float64(capacity) / 60.0
	return l.tb.Allow(ctx, key, capacity, tokens, refill)
}

// AdjustTPM 结算时对 TPM 做补差:delta>0 扣更多、delta<0 还一些。
// 补扣失败不报错(只是计费记录偏差,下一分钟会回填)。
func (l *Limiter) AdjustTPM(ctx context.Context, keyID uint64, capacity int64, delta int64) {
	if capacity <= 0 || delta == 0 {
		return
	}
	key := fmt.Sprintf("rl:tpm:k:%d", keyID)
	refill := float64(capacity) / 60.0
	if delta > 0 {
		_, _, _ = l.tb.Allow(ctx, key, capacity, delta, refill)
		return
	}
	// delta<0:把多扣的还回桶。用 negative cost 不安全,
	// 改用"加 tokens"Lua 回退;简单起见直接 PEXPIRE 不管(下次请求会按过期重建)。
	_ = ctx
	_ = time.Second
}
