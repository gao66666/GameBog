package handler

import "github.com/gin-gonic/gin"

// AuthUserID 从 JWT 中间件写入的 context 或 Authorization 头解析当前用户 ID。
func AuthUserID(c *gin.Context) uint64 {
	if c == nil {
		return 0
	}
	if v, ok := c.Get("userID"); ok && v != nil {
		switch id := v.(type) {
		case uint64:
			return id
		case int64:
			if id > 0 {
				return uint64(id)
			}
		case int:
			if id > 0 {
				return uint64(id)
			}
		case float64:
			if id > 0 {
				return uint64(id)
			}
		}
	}
	return OptionalJWTUserID(c)
}
