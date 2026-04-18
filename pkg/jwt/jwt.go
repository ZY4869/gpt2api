package jwt

import (
	"errors"
	"fmt"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
)

// Claims 是 JWT 的 payload。
type Claims struct {
	UserID uint64 `json:"uid"`
	Role   string `json:"role"`
	jwtv5.RegisteredClaims
}

// TokenPair 登录/刷新返回的双 token。
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// Config 依赖注入配置。
type Config struct {
	Secret        string
	Issuer        string
	AccessTTLSec  int
	RefreshTTLSec int
}

// Manager 负责签发与校验 access / refresh token。
// 通过 SetTTLProvider 可以让签发时使用的 TTL 来自外部热源(例如 system_settings)。
type Manager struct {
	secret      []byte
	issuer      string
	accessTTL   time.Duration
	refreshTTL  time.Duration
	ttlProvider func() (accessSec int, refreshSec int) // 可为 nil
}

func NewManager(c Config) *Manager {
	return &Manager{
		secret:     []byte(c.Secret),
		issuer:     c.Issuer,
		accessTTL:  time.Duration(c.AccessTTLSec) * time.Second,
		refreshTTL: time.Duration(c.RefreshTTLSec) * time.Second,
	}
}

// SetTTLProvider 注入一个热更 TTL 回调。回调返回秒;<=0 的值会被忽略(回退到启动时配置)。
func (m *Manager) SetTTLProvider(fn func() (accessSec int, refreshSec int)) {
	m.ttlProvider = fn
}

// currentTTLs 优先使用 provider 返回的值,否则用构造期固定值。
func (m *Manager) currentTTLs() (time.Duration, time.Duration) {
	access := m.accessTTL
	refresh := m.refreshTTL
	if m.ttlProvider != nil {
		a, r := m.ttlProvider()
		if a > 0 {
			access = time.Duration(a) * time.Second
		}
		if r > 0 {
			refresh = time.Duration(r) * time.Second
		}
	}
	return access, refresh
}

// Issue 签发一对 token。
func (m *Manager) Issue(userID uint64, role string) (*TokenPair, error) {
	now := time.Now()
	accessTTL, refreshTTL := m.currentTTLs()
	access, err := m.signWithTTL(userID, role, "access", now, accessTTL)
	if err != nil {
		return nil, err
	}
	refresh, err := m.signWithTTL(userID, role, "refresh", now, refreshTTL)
	if err != nil {
		return nil, err
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(accessTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

func (m *Manager) signWithTTL(userID uint64, role, typ string, now time.Time, ttl time.Duration) (string, error) {
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   fmt.Sprintf("%d", userID),
			Audience:  jwtv5.ClaimStrings{typ},
			IssuedAt:  jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(now.Add(ttl)),
			NotBefore: jwtv5.NewNumericDate(now),
		},
	}
	tok := jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, claims)
	return tok.SignedString(m.secret)
}

// Verify 验证 access token 并返回 claims。
func (m *Manager) Verify(tokenString string) (*Claims, error) {
	tok, err := jwtv5.ParseWithClaims(tokenString, &Claims{}, func(t *jwtv5.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwtv5.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// VerifyRefresh 验证 refresh token(多一步 audience 校验)。
func (m *Manager) VerifyRefresh(tokenString string) (*Claims, error) {
	claims, err := m.Verify(tokenString)
	if err != nil {
		return nil, err
	}
	for _, aud := range claims.Audience {
		if aud == "refresh" {
			return claims, nil
		}
	}
	return nil, errors.New("not a refresh token")
}
