package handler

import (
	"strconv"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *FollowHandler) CreateFollow(c *gin.Context) {
	p := new(models.ParamFollow)
	if err := c.ShouldBindJSON(p); err != nil {
		zap.L().Info("参数错误")
		tool.ResponseError(c, ErrInvalidParamFollow) // 返回错误响应
		return
	}
	err := h.se.CreateFollow(p)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, p, "关注成功")
}

// CreateFollowAuth 登录态关注：followerId 从 JWT 中间件读取，前端只需要传 followingId。
func (h *FollowHandler) CreateFollowAuth(c *gin.Context) {
	var req struct {
		FollowingID uint64 `json:"followingId,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.FollowingID == 0 {
		tool.ResponseError(c, ErrInvalidParamFollow)
		return
	}

	followerID := c.GetUint64("userID")
	p := &models.ParamFollow{FollowerID: followerID, FollowingID: req.FollowingID}
	if err := h.se.CreateFollow(p); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"followingId": strconv.FormatUint(req.FollowingID, 10)}, "关注成功")
}
