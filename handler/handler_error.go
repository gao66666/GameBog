package handler

import "github.com/gao66666/GoBlog/tool"

var (
	ErrInvalidParamFollow = tool.NewBizError(404, 40003, "不正确的关注参数")
	ErrCodeInvalidParam   = tool.NewBizError(404, 40001, "不正确的参数")
	ErrInvalidToken       = tool.NewBizError(404, 40001, "过期token")
	CodeServerBusy        = tool.NewBizError(404, 40001, "服务器繁忙")
)
