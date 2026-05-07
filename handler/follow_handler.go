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

// --- TopicFollow ---

// FollowTopicHandle 关注话题（body: { "topicId": "<id>" }）。
func (h *FollowHandler) FollowTopicHandle(c *gin.Context) {
	var req struct {
		TopicID uint `json:"topicId,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TopicID == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	userID := c.GetUint64("userID")
	if err := h.se.FollowTopicByTopicID(userID, req.TopicID); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "关注成功")
}

// UnfollowTopicHandle 取消关注话题（body: { "topicId": "<id>" }）。
func (h *FollowHandler) UnfollowTopicHandle(c *gin.Context) {
	var req struct {
		TopicID uint `json:"topicId,string" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TopicID == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	userID := c.GetUint64("userID")
	if err := h.se.UnfollowTopicByTopicID(userID, req.TopicID); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "已取消关注")
}

// GetMyFollowingUserCount 获取当前用户关注的「用户」数量（用于个人中心等展示）。
func (h *FollowHandler) GetMyFollowingUserCount(c *gin.Context) {
	userID := c.GetUint64("userID")
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	var n int64
	var err error
	if h.userSe != nil {
		n, err = h.userSe.GetFollowingUsersCountCached(userID)
	} else {
		n, err = h.se.CountFollowingUsers(userID)
	}
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"count": n}, "ok")
}

// ListMyFollowedTopicsHandle 获取我关注的话题列表（含 topicName）。
func (h *FollowHandler) ListMyFollowedTopicsHandle(c *gin.Context) {
	userID := c.GetUint64("userID")
	list, err := h.se.ListFollowedTopicsWithNames(userID)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"list": list})
}
