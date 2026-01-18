package service

import "github.com/gao66666/GoBlog/tool"

var (
	// --- 业务错误 (400 Range) ---
	ErrUserNotFound       = tool.NewBizError(404, 40001, "用户不存在")
	ErrUserAlreadyExists  = tool.NewBizError(400, 40002, "该电话号码已经被注册")
	ErrWrongPassword      = tool.NewBizError(400, 40003, "密码校验失败")
	ErrRegistrationFailed = tool.NewBizError(400, 40004, "注册信息有误，请重试")

	// --- 系统内部错误 (500 Range) ---
	ErrInternalServer = tool.NewBizError(500, 50000, "系统繁忙，请稍后再试")

	// 仅供 Service 层内部逻辑判断使用（不推荐直接返给前端）
	ErrHash          = tool.NewBizError(500, 50001, "安全加密失败")
	ErrTokenGenerate = tool.NewBizError(500, 50002, "登录凭证生成失败")
	ErrDataSelect    = tool.NewBizError(500, 50003, "数据库读取异常")
	ErrDataInsert    = tool.NewBizError(500, 50004, "数据库写入异常")
	ErrHashGenerate  = tool.NewBizError(500, 50005, "密码加密失败")
	ErrGetRootIDS    = tool.NewBizError(500, 50006, "rootids获取失败")
)
