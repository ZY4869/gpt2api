package ratelimit

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// TokenBucket 基于 Redis 的令牌桶限流器。
// 使用 Lua 脚本保证原子性,适合 RPM / TPM 场景。
type TokenBucket struct {
	client *redis.Client
}

func NewTokenBucket(c *redis.Client) *TokenBucket { return &TokenBucket{client: c} }

// script 参数:
//   KEYS[1] = bucket key
//   ARGV[1] = capacity(桶容量)
//   ARGV[2] = refill_rate(每秒回填令牌数)
//   ARGV[3] = now_ms(当前毫秒)
//   ARGV[4] = cost(本次消费)
// 返回:{allowed(0/1), remaining(字符串)}
var script = redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local cost = tonumber(ARGV[4])

local bucket = redis.call("HMGET", key, "tokens", "ts")
local tokens = tonumber(bucket[1])
local ts = tonumber(bucket[2])
if tokens == nil then
    tokens = capacity
    ts = now
end

local delta = (now - ts) / 1000.0 * refill
tokens = math.min(capacity, tokens + delta)

local allowed = 0
if tokens >= cost then
    tokens = tokens - cost
    allowed = 1
end

redis.call("HMSET", key, "tokens", tostring(tokens), "ts", now)
redis.call("PEXPIRE", key, math.ceil(capacity / math.max(refill, 0.001) * 2000))

return {allowed, tostring(tokens)}
`)

// Allow 尝试消费 cost 个令牌。返回 (是否允许, 剩余令牌)。
func (t *TokenBucket) Allow(ctx context.Context, key string, capacity, cost int64, refillPerSec float64) (bool, float64, error) {
	nowMs := time.Now().UnixMilli()
	res, err := script.Run(ctx, t.client, []string{key}, capacity, refillPerSec, nowMs, cost).Slice()
	if err != nil {
		return false, 0, err
	}
	allowedI, _ := res[0].(int64)
	allowed := allowedI == 1
	var remaining float64
	switch v := res[1].(type) {
	case string:
		remaining, _ = strconv.ParseFloat(v, 64)
	case int64:
		remaining = float64(v)
	}
	return allowed, remaining, nil
}
