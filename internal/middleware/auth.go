package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/432539/gpt2api/internal/rbac"
	pkgjwt "github.com/432539/gpt2api/pkg/jwt"
	"github.com/432539/gpt2api/pkg/resp"
)

const (
	CtxUserID = "user_id"
	CtxRole   = "role"
)

// JWTAuth 校验 Bearer JWT,把 user_id / role 注入 context。
func JWTAuth(jm *pkgjwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		hdr := c.GetHeader("Authorization")
		if !strings.HasPrefix(hdr, "Bearer ") {
			resp.Unauthorized(c, "missing bearer token")
			return
		}
		tokenStr := strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer "))
		claims, err := jm.Verify(tokenStr)
		if err != nil {
			resp.Unauthorized(c, "invalid token: "+err.Error())
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxRole, claims.Role)
		c.Next()
	}
}

// RequireAdmin 在 JWTAuth 之后使用,校验 role==admin。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !rbac.IsAdmin(Role(c)) {
			resp.Forbidden(c, "admin only")
			return
		}
		c.Next()
	}
}

// RequirePerm 要求当前角色拥有指定的任一权限(OR 语义)。
// 匿名或无 JWT 的请求(UserID==0)直接 401 以防止权限检查逻辑被绕过。
func RequirePerm(perms ...rbac.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		if UserID(c) == 0 {
			resp.Unauthorized(c, "not authenticated")
			return
		}
		role := Role(c)
		if !rbac.HasAny(role, perms...) {
			resp.Forbidden(c, "insufficient permission")
			return
		}
		c.Next()
	}
}

// RequireAllPerms 要求拥有全部权限(AND 语义,罕用)。
func RequireAllPerms(perms ...rbac.Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		if UserID(c) == 0 {
			resp.Unauthorized(c, "not authenticated")
			return
		}
		role := Role(c)
		if !rbac.HasAll(role, perms...) {
			resp.Forbidden(c, "insufficient permission")
			return
		}
		c.Next()
	}
}

// UserID 从 context 取 user_id,找不到返回 0。
func UserID(c *gin.Context) uint64 {
	v, ok := c.Get(CtxUserID)
	if !ok {
		return 0
	}
	if uid, ok := v.(uint64); ok {
		return uid
	}
	return 0
}

// Role 从 context 取 role,未登录返回空串。
func Role(c *gin.Context) string {
	v, ok := c.Get(CtxRole)
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
