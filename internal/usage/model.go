package usage

import "time"

// Type 业务类型。
const (
	TypeChat  = "chat"
	TypeImage = "image"
)

// Status 请求结果。
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// Log 对应 usage_logs 表一行。
type Log struct {
	UserID           uint64    `db:"user_id"`
	KeyID            uint64    `db:"key_id"`
	ModelID          uint64    `db:"model_id"`
	AccountID        uint64    `db:"account_id"`
	RequestID        string    `db:"request_id"`
	Type             string    `db:"type"`
	InputTokens      int       `db:"input_tokens"`
	OutputTokens     int       `db:"output_tokens"`
	CacheReadTokens  int       `db:"cache_read_tokens"`
	CacheWriteTokens int       `db:"cache_write_tokens"`
	ImageCount       int       `db:"image_count"`
	CreditCost       int64     `db:"credit_cost"`
	DurationMs       int       `db:"duration_ms"`
	Status           string    `db:"status"`
	ErrorCode        string    `db:"error_code"`
	IP               string    `db:"ip"`
	UA               string    `db:"ua"`
	CreatedAt        time.Time `db:"created_at"`
}
