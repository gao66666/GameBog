package database

import "github.com/gao66666/GoBlog/tool"

var (
	ErrInitFollow = tool.NewBizError(404, 40009, "评论数据库初始化出错")

	ErrInitComment = tool.NewBizError(404, 40001, "评论数据库初始化出错")

	ErrInitUser = tool.NewBizError(404, 40001, "用户数据库初始化出错")

	ErrCreatUser  = tool.NewBizError(404, 40001, "数据库新增用户出错")
	ErrHash       = tool.NewBizError(404, 40001, "密码哈希出错")
	ErrUserExists = tool.NewBizError(404, 40001, "该电话号码已经注册")
	ErrUserGet    = tool.NewBizError(404, 40001, "无法查询到该用户")
)
