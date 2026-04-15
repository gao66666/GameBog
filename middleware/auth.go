package middleware

import (
	"errors"
	"strings"

	"github.com/gao66666/GoBlog/jwt_module" // 引入你刚才定义的包
	"github.com/gao66666/GoBlog/tool"       // 引入你刚才定义的包
	"github.com/gin-gonic/gin"
)

func JWTAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 获取 Header 中的 Authorization
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// 这里可以直接用你的 ResponseError 逻辑，或者简单处理
			tool.ResponseError(c, jwt_module.ErrInvalidToken)
			c.Abort() // 拦截请求，不执行后续逻辑
			return
		}

		// 2. 按空格拆分，检查 Bearer 格式
		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			tool.ResponseError(c, jwt_module.ErrInvalidToken)
			c.Abort()
			return
		}

		// 3. 解析 Token (调用你 jwt_module 里的方法)
		claims, err := jwt_module.ParseToken(parts[1])
		if err != nil {
			// 根据错误类型返回对应的业务错误
			if errors.Is(err, jwt_module.ErrExpiredToken) {
				tool.ResponseError(c, jwt_module.ErrExpiredToken)
			} else {
				tool.ResponseError(c, jwt_module.ErrInvalidToken)
			}
			c.Abort()
			return
		}

		// 4. 将解析出的用户信息存入上下文，方便后续 Handler 获取
		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}
