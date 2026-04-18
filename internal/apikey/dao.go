package apikey

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("apikey: not found")

type DAO struct{ db *sqlx.DB }

func NewDAO(db *sqlx.DB) *DAO { return &DAO{db: db} }

func (d *DAO) Create(ctx context.Context, k *APIKey) (uint64, error) {
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO api_keys
         (user_id, name, key_prefix, key_hash, quota_limit, allowed_models, allowed_ips,
          rpm, tpm, expires_at, enabled)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.UserID, k.Name, k.KeyPrefix, k.KeyHash, k.QuotaLimit,
		k.AllowedModels, k.AllowedIPs, k.RPM, k.TPM, k.ExpiresAt, k.Enabled,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func (d *DAO) GetByID(ctx context.Context, id uint64) (*APIKey, error) {
	var k APIKey
	err := d.db.GetContext(ctx, &k,
		`SELECT * FROM api_keys WHERE id = ? AND deleted_at IS NULL`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &k, err
}

// GetByHash 按 SHA-256 查询。只返回 enabled && 未删除的 key。
func (d *DAO) GetByHash(ctx context.Context, hash string) (*APIKey, error) {
	var k APIKey
	err := d.db.GetContext(ctx, &k,
		`SELECT * FROM api_keys
         WHERE key_hash = ? AND enabled = 1 AND deleted_at IS NULL`, hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &k, err
}

func (d *DAO) ListByUser(ctx context.Context, userID uint64, offset, limit int) ([]*APIKey, int64, error) {
	var total int64
	if err := d.db.GetContext(ctx, &total,
		`SELECT COUNT(*) FROM api_keys WHERE user_id = ? AND deleted_at IS NULL AND name <> ?`,
		userID, InternalKeyName); err != nil {
		return nil, 0, err
	}
	rows := make([]*APIKey, 0, limit)
	err := d.db.SelectContext(ctx, &rows,
		`SELECT * FROM api_keys WHERE user_id = ? AND deleted_at IS NULL AND name <> ?
         ORDER BY id DESC LIMIT ? OFFSET ?`,
		userID, InternalKeyName, limit, offset)
	return rows, total, err
}

// CountActiveByUser 返回用户当前"可见且可用" key 数量(未删 + 非 internal)。
// 用于 max_per_user 配额判定。
func (d *DAO) CountActiveByUser(ctx context.Context, userID uint64) (int, error) {
	var n int
	err := d.db.GetContext(ctx, &n,
		`SELECT COUNT(*) FROM api_keys
           WHERE user_id = ? AND deleted_at IS NULL AND name <> ?`,
		userID, InternalKeyName)
	return n, err
}

// GetByUserAndName 按 user_id + name 精确找一把 key(不关心 enabled),供内部 key 查询使用。
func (d *DAO) GetByUserAndName(ctx context.Context, userID uint64, name string) (*APIKey, error) {
	var k APIKey
	err := d.db.GetContext(ctx, &k,
		`SELECT * FROM api_keys
         WHERE user_id = ? AND name = ? AND deleted_at IS NULL
         ORDER BY id DESC LIMIT 1`,
		userID, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &k, err
}

func (d *DAO) Update(ctx context.Context, k *APIKey) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE api_keys
         SET name=?, quota_limit=?, allowed_models=?, allowed_ips=?,
             rpm=?, tpm=?, expires_at=?, enabled=?
         WHERE id = ? AND user_id = ? AND deleted_at IS NULL`,
		k.Name, k.QuotaLimit, k.AllowedModels, k.AllowedIPs,
		k.RPM, k.TPM, k.ExpiresAt, k.Enabled, k.ID, k.UserID,
	)
	return err
}

func (d *DAO) SoftDelete(ctx context.Context, userID, id uint64) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE api_keys SET deleted_at = ? WHERE id = ? AND user_id = ?`,
		time.Now(), id, userID)
	return err
}

// TouchUsage 更新最后使用时间/IP 和累计额度(原子)。
func (d *DAO) TouchUsage(ctx context.Context, id uint64, ip string, addUsed int64) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE api_keys
         SET last_used_at = ?, last_used_ip = ?, quota_used = quota_used + ?
         WHERE id = ?`,
		time.Now(), ip, addUsed, id)
	return err
}
