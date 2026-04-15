package handler

import (
	"strconv"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func (h *UserHandler) LoginHandle(c *gin.Context) {
	var param models.ParamLogin
	if err := c.ShouldBindJSON(&param); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	//登录成功，用户不存在，登录密码错误、数据库查询失败
	user, token, err := h.se.Login(param)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"user_id": strconv.FormatUint(user.ID, 10), "user_name": user.Name, "token": token}, "登录成功")
}

func (hd *UserHandler) SignUpHandle(c *gin.Context) {
	var param models.ParamSignUp
	if err := c.ShouldBindJSON(&param); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	// 调用 Service，获取 ID 和 业务错误
	userID, err := hd.se.SignUp(param)

	if err != nil {
		// 只根据 Service 层的业务错误做判断
		tool.ResponseError(c, err)
		return
	}

	// 成功情况
	tool.ResponseSuccess(c, gin.H{"user_id": strconv.FormatUint(userID, 10)}, "用户注册成功")
}

func (hd *UserHandler) UpdateUserHandle(c *gin.Context) {
	// 1. 获取当前登录用户 ID (从 JWT 中间件解析出来的)
	uid, _ := c.Get("userID")
	userID := uid.(uint64)

	// 2. 绑定参数
	var p models.UpdateUserParam
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	// 3. 将 struct 转换为 map，实现“传哪个改哪个”
	updateMap := make(map[string]interface{})
	if p.Name != nil {
		updateMap["name"] = *p.Name
	}
	if p.Tel != nil {
		updateMap["tel"] = *p.Tel
	}
	if p.PassWord != nil {
		// 因为 p.PassWord 是 *string，所以需要用 *p.PassWord 拿到真正的字符串内容
		// 然后再用 []byte() 转换成字节切片
		hashedBytes, err := bcrypt.GenerateFromPassword([]byte(*p.PassWord), bcrypt.DefaultCost)
		if err != nil {
			tool.ResponseErrorWithMsg(c, "错误的密码格式")
			return
		}
		// 存入 map，准备更新数据库
		updateMap["password"] = string(hashedBytes)
	}
	if p.Email != nil {
		updateMap["email"] = *p.Email
	}

	// 如果用户什么都没传，直接返回成功
	if len(updateMap) == 0 {
		tool.ResponseSuccess(c, nil, "没有需要更新的内容")
		return
	}

	// 4. 调用 Service 执行更新
	if err := hd.se.UpdateUser(userID, updateMap); err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	tool.ResponseSuccess(c, nil, "更新成功")
}

// GetUserPublic 公开查询用户信息（用于文章详情展示作者信息）。
func (h *UserHandler) GetUserPublic(c *gin.Context) {
	idStr := c.Param("id")
	uid, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || uid == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	u, err := h.se.GetUserByID(uid)
	if err != nil {
		tool.ResponseErrorWithMsg(c, "用户不存在")
		return
	}

	tool.ResponseSuccess(c, gin.H{
		"user_id":         strconv.FormatUint(u.ID, 10),
		"user_name":       u.Name,
		"avatar":          u.Avatar,
		"follower_count":  u.FollowingCount,
	}, "查询成功")
}
