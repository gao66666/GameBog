package handler

import (
	"net/http"
	"strconv"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
)

type PointsHandler struct {
	se *service.PointsService
}

func NewPointsHandler(se *service.PointsService) *PointsHandler {
	return &PointsHandler{se: se}
}

// GetMyWallet 获取自己的积分账户
func (h *PointsHandler) GetMyWallet(c *gin.Context) {
	userID := c.GetUint64("userID")
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	wallet, err := h.se.GetWallet(userID)
	if err != nil {
		if err == database.ErrWalletNotFound {
			// 未创建钱包等价于余额 0
			tool.ResponseSuccess(c, gin.H{
				"balance":       0,
				"frozenBalance": 0,
			})
			return
		}
		tool.ResponseError(c, err)
		return
	}

	tool.ResponseSuccess(c, gin.H{
		"balance":       wallet.Balance,
		"frozenBalance": wallet.FrozenBalance,
	})
}

// GetUserWallet 获取用户的积分信息（公开）
func (h *PointsHandler) GetUserWallet(c *gin.Context) {
	idStr := c.Param("id")
	userID, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	wallet, err := h.se.GetWallet(userID)
	if err != nil {
		if err == database.ErrWalletNotFound {
			tool.ResponseSuccess(c, gin.H{
				"balance":       0,
				"frozenBalance": 0,
			})
			return
		}
		tool.ResponseError(c, err)
		return
	}

	tool.ResponseSuccess(c, gin.H{
		"balance":       wallet.Balance,
		"frozenBalance": wallet.FrozenBalance,
	})
}

// Checkin 签到
func (h *PointsHandler) Checkin(c *gin.Context) {
	userID := c.GetUint64("userID")
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	var req struct {
		UserID uint64 `json:"user_id"`
	}
	if err := c.ShouldBindJSON(&req); err == nil && req.UserID != 0 {
		userID = req.UserID
	}

	if err := h.se.Checkin(userID); err != nil {
		tool.ResponseError(c, err)
		return
	}

	// 返回签到后的积分
	wallet, _ := h.se.GetWallet(userID)
	tool.ResponseSuccess(c, gin.H{
		"message": "签到成功",
		"balance": wallet.Balance,
	})
}

// CheckinViaQuery 签到（GET 方式，前端轮询或触发）
func (h *PointsHandler) CheckinViaQuery(c *gin.Context) {
	userID := c.GetUint64("userID")
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	if err := h.se.Checkin(userID); err != nil {
		tool.ResponseError(c, err)
		return
	}

	wallet, _ := h.se.GetWallet(userID)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "签到成功",
		"data": gin.H{
			"balance": wallet.Balance,
		},
	})
}
