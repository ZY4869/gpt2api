package apikey

import (
	"database/sql"
	"time"
)

// APIKey 对应 api_keys 表。
type APIKey struct {
	ID            uint64         `db:"id" json:"id"`
	UserID        uint64         `db:"user_id" json:"user_id"`
	Name          string         `db:"name" json:"name"`
	KeyPrefix     string         `db:"key_prefix" json:"key_prefix"`
	KeyHash       string         `db:"key_hash" json:"-"`
	QuotaLimit    int64          `db:"quota_limit" json:"quota_limit"`
	QuotaUsed     int64          `db:"quota_used" json:"quota_used"`
	AllowedModels sql.NullString `db:"allowed_models" json:"allowed_models,omitempty"`
	AllowedIPs    sql.NullString `db:"allowed_ips" json:"allowed_ips,omitempty"`
	RPM           int            `db:"rpm" json:"rpm"`
	TPM           int64          `db:"tpm" json:"tpm"`
	ExpiresAt     sql.NullTime   `db:"expires_at" json:"expires_at,omitempty"`
	Enabled       bool           `db:"enabled" json:"enabled"`
	LastUsedAt    sql.NullTime   `db:"last_used_at" json:"last_used_at,omitempty"`
	LastUsedIP    string         `db:"last_used_ip" json:"last_used_ip"`
	CreatedAt     time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time      `db:"updated_at" json:"updated_at"`
	DeletedAt     sql.NullTime   `db:"deleted_at" json:"-"`
}
