package jwt_module

import (
	"errors"
	"time"

	"github.com/gao66666/GoBlog/setting"
	"github.com/gao66666/GoBlog/tool"
	"github.com/golang-jwt/jwt/v5" // 注意这里带 v5
)

var (
	ErrInvalidToken = tool.NewBizError(401, 40002, "不正确的token")
	ErrExpiredToken = tool.NewBizError(401, 40004, "过期token")
)

// 1. 定义自己的负载结构体
type MyClaims struct {
	UserID   uint64 `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

func secret() []byte {
	if setting.Conf != nil && setting.Conf.AuthConfig != nil && setting.Conf.AuthConfig.JWTSecret != "" {
		return []byte(setting.Conf.AuthConfig.JWTSecret)
	}
	return []byte("change-me-in-production")
}

func expireHours() time.Duration {
	if setting.Conf != nil && setting.Conf.AuthConfig != nil && setting.Conf.AuthConfig.JWTExpireHours > 0 {
		return time.Duration(setting.Conf.AuthConfig.JWTExpireHours) * time.Hour
	}
	return 24 * time.Hour
}

// 2. 生成 Token
func GenToken(userID uint64, username string) (string, error) {
	claims := MyClaims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			// v5 使用 NumericDate 类型，通过 jwt.NewNumericDate 转换
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expireHours())),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "my-blog",
		},
	}
	// 注意：v5 的加密常量依然是 jwt.SigningMethodHS256
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(secret())
}
func ParseToken(tokenStr string) (*MyClaims, error) {
	var mc = new(MyClaims)
	token, err := jwt.ParseWithClaims(tokenStr, mc, func(token *jwt.Token) (interface{}, error) {
		return secret(), nil
	})

	if err != nil {
		// v5 细分错误类型的写法
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}

	if token.Valid {
		return mc, nil
	}
	return nil, ErrInvalidToken
}
