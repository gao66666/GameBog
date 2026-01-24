package database

import "github.com/gao66666/GoBlog/tool"

var (
	ErrInitFollow      = tool.NewBizError(404, 40009, "关注数据库初始化出错")
	ErrNullParamFollow = tool.NewBizError(404, 40003, "follower_id 和 following_id 不能为空")
	ErrFollowSelf      = tool.NewBizError(404, 40003, "不能关注自己")
	ErrDoubleFollow    = tool.NewBizError(404, 40003, "不能重复关注")
	ErrSQLFollow       = tool.NewBizError(404, 40003, "数据库关注失败")

	ErrInitComment = tool.NewBizError(404, 40001, "评论数据库初始化出错")

	ErrInitUser = tool.NewBizError(404, 40001, "用户数据库初始化出错")

	ErrCreatUser  = tool.NewBizError(404, 40001, "数据库新增用户出错")
	ErrHash       = tool.NewBizError(404, 40001, "密码哈希出错")
	ErrUserExists = tool.NewBizError(404, 40001, "该电话号码已经注册")
	ErrUserGet    = tool.NewBizError(404, 40001, "无法查询到该用户")
)
