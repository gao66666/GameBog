package handler

import "github.com/gao66666/GoBlog/tool"

var (
	ErrInvalidParamFollow = tool.NewBizError(400, 40003, "不正确的关注参数")
	ErrCodeInvalidParam   = tool.NewBizError(400, 40001, "不正确的参数")
	ErrInvalidToken = tool.NewBizError(401, 40002, "过期token")
	CodeServerBusy  = tool.NewBizError(500, 50001, "服务器繁忙")
)
