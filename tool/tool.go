package tool

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type BizError struct {
	HttpCode int    `json:"-"`       // HTTP 状态码
	Code     int    `json:"code"`    // 业务自定义错误码
	Message  string `json:"message"` // 暴露给前端的消息
}

func (e *BizError) Error() string {
	return e.Message
}

// 快速新建错误的函数
func NewBizError(httpCode, bizCode int, msg string) *BizError {
	return &BizError{
		HttpCode: httpCode,
		Code:     bizCode,
		Message:  msg,
	}
}

func ResponseError(c *gin.Context, err error) {
	if err == nil {
		return
	}

	var bizErr *BizError
	if errors.As(err, &bizErr) {
		// 如果是业务错误，使用其定义的 HttpCode 和内容
		c.JSON(bizErr.HttpCode, bizErr)
		return
	}

	zap.L().Error("系统内部错误",
		zap.Error(err),                         // 记录错误对象本身
		zap.String("path", c.Request.URL.Path), // 记录出错的请求路径
	)

	c.JSON(http.StatusInternalServerError, gin.H{
		"code":    50000,
		"message": "服务器开小差了，请稍后再试",
	})
}
