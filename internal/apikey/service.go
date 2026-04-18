package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/432539/gpt2api/internal/settings"
)

// Service API Key 业务。
type Service struct {
	dao      *DAO
	settings *settings.Service // 可为 nil(兼容旧调用方)
}

func NewService(dao *DAO) *Service { return &Service{dao: dao} }

// SetSettings 注入系统设置,用于 max_per_user / 默认日配额。
func (s *Service) SetSettings(ss *settings.Service) { s.settings = ss }

// ErrKeyCountLimit 用户已达到可创建 key 数上限。
var ErrKeyCountLimit = errors.New("apikey: per-user key count limit reached")

// InternalKeyName 在线体验专用内部 key 的固定名称。
// 该名称的 key 不会出现在用户前端列表,也不计入配额。
const InternalKeyName = "__playground__"

// EnsureInternalKey 按 user_id 懒加载一把内部 playground key(没有就创建)。
// 返回的 APIKey 可以直接喂给 gateway handler,复用扣费/限流等逻辑。
// 该 key 的明文被丢弃(内部不暴露 raw 值,网关只用 k.ID/k.UserID 做计费)。
func (s *Service) EnsureInternalKey(ctx context.Context, userID uint64) (*APIKey, error) {
	if k, err := s.dao.GetByUserAndName(ctx, userID, InternalKeyName); err == nil {
		return k, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	raw, err := generateSecret()
	if err != nil {
		return nil, err
	}
	hash := HashKey(raw)
	prefix := raw
	if len(prefix) > 11 {
		prefix = prefix[:11]
	}
	k := &APIKey{
		UserID:    userID,
		Name:      InternalKeyName,
		KeyPrefix: prefix,
		KeyHash:   hash,
		Enabled:   true,
	}
	id, err := s.dao.Create(ctx, k)
	if err != nil {
		return nil, err
	}
	k.ID = id
	return k, nil
}

// CreateInput 创建 Key 入参(不含 key 本身)。
type CreateInput struct {
	Name          string    `json:"name"`
	QuotaLimit    int64     `json:"quota_limit"`
	AllowedModels []string  `json:"allowed_models"`
	AllowedIPs    []string  `json:"allowed_ips"`
	RPM           int       `json:"rpm"`
	TPM           int64     `json:"tpm"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// UpdateInput 更新入参。
type UpdateInput struct {
	Name          string    `json:"name"`
	QuotaLimit    int64     `json:"quota_limit"`
	AllowedModels []string  `json:"allowed_models"`
	AllowedIPs    []string  `json:"allowed_ips"`
	RPM           int       `json:"rpm"`
	TPM           int64     `json:"tpm"`
	ExpiresAt     time.Time `json:"expires_at"`
	Enabled       *bool     `json:"enabled"`
}

// GeneratedKey 创建时返回,明文 key 仅此一次暴露。
type GeneratedKey struct {
	Key    string  `json:"key"`
	Record *APIKey `json:"record"`
}

// generate 随机生成 sk-xxxx(32 byte base62)key。
func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	s := base64.RawURLEncoding.EncodeToString(buf)
	// 去掉连字符防止歧义。
	s = strings.ReplaceAll(s, "-", "x")
	s = strings.ReplaceAll(s, "_", "y")
	return "sk-" + s, nil
}

// HashKey 计算 SHA-256(key) 的 hex。
func HashKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

func jsonListNullable(list []string) sql.NullString {
	if len(list) == 0 {
		return sql.NullString{}
	}
	b, _ := json.Marshal(list)
	return sql.NullString{String: string(b), Valid: true}
}

func (s *Service) Create(ctx context.Context, userID uint64, in CreateInput) (*GeneratedKey, error) {
	// 单用户 key 数上限(0=不限)
	if s.settings != nil {
		if cap := s.settings.KeyMaxPerUser(); cap > 0 {
			cur, err := s.dao.CountActiveByUser(ctx, userID)
			if err != nil {
				return nil, err
			}
			if cur >= cap {
				return nil, ErrKeyCountLimit
			}
		}
		// 未显式指定 QuotaLimit 时,应用默认日配额
		if in.QuotaLimit <= 0 {
			in.QuotaLimit = s.settings.KeyDefaultDailyQuota()
		}
	}

	key, err := generateSecret()
	if err != nil {
		return nil, err
	}
	hash := HashKey(key)
	prefix := key
	if len(prefix) > 11 { // "sk-" + 8
		prefix = prefix[:11]
	}

	k := &APIKey{
		UserID:        userID,
		Name:          in.Name,
		KeyPrefix:     prefix,
		KeyHash:       hash,
		QuotaLimit:    in.QuotaLimit,
		AllowedModels: jsonListNullable(in.AllowedModels),
		AllowedIPs:    jsonListNullable(in.AllowedIPs),
		RPM:           in.RPM,
		TPM:           in.TPM,
		Enabled:       true,
	}
	if !in.ExpiresAt.IsZero() {
		k.ExpiresAt = sql.NullTime{Time: in.ExpiresAt, Valid: true}
	}
	id, err := s.dao.Create(ctx, k)
	if err != nil {
		return nil, err
	}
	k.ID = id
	return &GeneratedKey{Key: key, Record: k}, nil
}

func (s *Service) Update(ctx context.Context, userID, id uint64, in UpdateInput) (*APIKey, error) {
	k, err := s.dao.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if k.UserID != userID {
		return nil, errors.New("forbidden")
	}
	if in.Name != "" {
		k.Name = in.Name
	}
	if in.QuotaLimit >= 0 {
		k.QuotaLimit = in.QuotaLimit
	}
	if in.AllowedModels != nil {
		k.AllowedModels = jsonListNullable(in.AllowedModels)
	}
	if in.AllowedIPs != nil {
		k.AllowedIPs = jsonListNullable(in.AllowedIPs)
	}
	if in.RPM >= 0 {
		k.RPM = in.RPM
	}
	if in.TPM >= 0 {
		k.TPM = in.TPM
	}
	if !in.ExpiresAt.IsZero() {
		k.ExpiresAt = sql.NullTime{Time: in.ExpiresAt, Valid: true}
	}
	if in.Enabled != nil {
		k.Enabled = *in.Enabled
	}
	if err := s.dao.Update(ctx, k); err != nil {
		return nil, err
	}
	return k, nil
}

func (s *Service) Delete(ctx context.Context, userID, id uint64) error {
	return s.dao.SoftDelete(ctx, userID, id)
}

func (s *Service) List(ctx context.Context, userID uint64, offset, limit int) ([]*APIKey, int64, error) {
	return s.dao.ListByUser(ctx, userID, offset, limit)
}

// TouchUsage 更新 API Key 最近使用信息与累计扣费,供网关完成结算后调用。
func (s *Service) TouchUsage(ctx context.Context, id uint64, lastIP string, deltaQuota int64) error {
	return s.dao.TouchUsage(ctx, id, lastIP, deltaQuota)
}

// Verify 网关鉴权核心:按明文 key 返回 APIKey(或错误)。
// 判定:enabled / 过期 / quota 剩余 / IP 白名单 / model 白名单 在调用方分步做。
func (s *Service) Verify(ctx context.Context, key string) (*APIKey, error) {
	if !strings.HasPrefix(key, "sk-") {
		return nil, errors.New("API Key 格式不正确")
	}
	hash := HashKey(key)
	k, err := s.dao.GetByHash(ctx, hash)
	if err != nil {
		return nil, errors.New("API Key 不存在或已被停用")
	}
	if k.ExpiresAt.Valid && time.Now().After(k.ExpiresAt.Time) {
		return nil, errors.New("API Key 已过期")
	}
	if k.QuotaLimit > 0 && k.QuotaUsed >= k.QuotaLimit {
		return nil, errors.New("API Key 配额已用完")
	}
	return k, nil
}

// ModelAllowed 判断 model 是否在白名单内。
func (k *APIKey) ModelAllowed(slug string) bool {
	if !k.AllowedModels.Valid || k.AllowedModels.String == "" {
		return true
	}
	var list []string
	if err := json.Unmarshal([]byte(k.AllowedModels.String), &list); err != nil {
		return true
	}
	if len(list) == 0 {
		return true
	}
	for _, s := range list {
		if s == slug {
			return true
		}
	}
	return false
}

// IPAllowed 判断 ip 是否在白名单内。
func (k *APIKey) IPAllowed(ip string) bool {
	if !k.AllowedIPs.Valid || k.AllowedIPs.String == "" {
		return true
	}
	var list []string
	if err := json.Unmarshal([]byte(k.AllowedIPs.String), &list); err != nil {
		return true
	}
	if len(list) == 0 {
		return true
	}
	for _, s := range list {
		if s == ip {
			return true
		}
	}
	return false
}

// DAO 暴露以便网关调用 TouchUsage。
func (s *Service) DAO() *DAO { return s.dao }
