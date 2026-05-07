package handler

import (
	"strings"

	"github.com/gao66666/GoBlog/jwt_module"
	"github.com/gin-gonic/gin"
)

// OptionalJWTUserID 从 Authorization Bearer 解析用户 ID；无 token 或无效时返回 0。
func OptionalJWTUserID(c *gin.Context) uint64 {
	auth := c.GetHeader("Authorization")
	if auth == "" {
		return 0
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return 0
	}
	claims, err := jwt_module.ParseToken(parts[1])
	if err != nil || claims == nil || claims.UserID == 0 {
		return 0
	}
	return claims.UserID
}
