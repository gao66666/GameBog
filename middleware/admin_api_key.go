package middleware

import (
	"crypto/subtle"
	"strings"

	"github.com/gao66666/GoBlog/setting"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
)

var (
	errAdminUnauthorized  = tool.NewBizError(401, 40103, "无效或缺失的管理密钥")
	errAdminNotConfigured = tool.NewBizError(503, 50303, "后台 API 未配置管理密钥")
)

// AdminAPIKey 校验请求头 X-Admin-Key，与 security.admin_api_secret / ADMIN_API_SECRET 一致。
func AdminAPIKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		sec := setting.Conf.SecurityConfig
		expected := ""
		if sec != nil {
			expected = strings.TrimSpace(sec.AdminAPISecret)
		}
		if expected == "" {
			tool.ResponseError(c, errAdminNotConfigured)
			c.Abort()
			return
		}
		got := strings.TrimSpace(c.GetHeader("X-Admin-Key"))
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			tool.ResponseError(c, errAdminUnauthorized)
			c.Abort()
			return
		}
		c.Next()
	}
}
