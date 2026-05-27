package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/mq"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type PointsHandler struct {
	se *service.PointsService
}

func NewPointsHandler(se *service.PointsService) *PointsHandler {
	return &PointsHandler{se: se}
}

// ProcessPointsEarnMessage 实现 mq.PointsEarnProcessor：Kafka 消费入账（幂等键在消息体内）。
func (h *PointsHandler) ProcessPointsEarnMessage(ctx context.Context, payload []byte) error {
	_ = ctx
	if h == nil || h.se == nil {
		return nil
	}
	var p mq.PointsEarnPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		zap.L().Error("积分入账消息解析失败", zap.Error(err))
		return nil
	}
	if p.UserID == 0 || p.RefType == "" || p.TxnID == 0 {
		return nil
	}
	return h.se.EarnPointsWithTxnID(p.UserID, p.RefType, p.RefID, p.TxnID)
}

// GetMyWallet 获取自己的积分账户。
// warm_redis=1（或 from_db=1）：从 MySQL 加载并写入 Redis，再返回 Redis 中的余额（积分商城用）。
func (h *PointsHandler) GetMyWallet(c *gin.Context) {
	userID := AuthUserID(c)
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	warmRedis := c.Query("warm_redis") == "1" || c.Query("warm_redis") == "true" ||
		c.Query("from_db") == "1" || c.Query("from_db") == "true"

	var wallet *models.UserWallet
	var err error
	source := "db"

	if warmRedis {
		if _, err = h.se.WarmWalletRedisFromDB(userID); err != nil {
			tool.ResponseError(c, err)
			return
		}
		if w, hit, rerr := h.se.GetWalletFromRedis(userID); hit && rerr == nil {
			wallet = w
			source = "redis"
		} else {
			wallet, err = h.se.GetWalletFromDB(userID)
			source = "db"
		}
	} else if w, hit, rerr := h.se.GetWalletFromRedis(userID); hit && rerr == nil {
		wallet = w
		source = "redis"
	} else {
		wallet, err = h.se.EnsureWallet(userID)
		source = "db"
	}

	if err != nil {
		tool.ResponseError(c, err)
		return
	}

	tool.ResponseSuccess(c, gin.H{
		"balance":       wallet.Balance,
		"frozenBalance": wallet.FrozenBalance,
		"source":        source,
		"warm_redis":    warmRedis,
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

	wallet, err := h.se.EnsureWallet(userID)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}

	tool.ResponseSuccess(c, gin.H{
		"balance":       wallet.Balance,
		"frozenBalance": wallet.FrozenBalance,
	})
}

// ListMyTransactions 积分流水（分页）：按当前登录 user_id 查 points_transactions。
func (h *PointsHandler) ListMyTransactions(c *gin.Context) {
	userID := AuthUserID(c)
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	list, total, err := h.se.ListMyTransactions(userID, page, size)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	if list == nil {
		list = []models.PointsTransaction{}
	}
	tool.ResponseSuccess(c, gin.H{
		"list":    list,
		"total":   total,
		"page":    page,
		"size":    size,
		"user_id": strconv.FormatUint(userID, 10),
	}, "ok")
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
	_ = h.se.InvalidateWalletRedis(userID)
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
	_ = h.se.InvalidateWalletRedis(userID)
	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "签到成功",
		"data": gin.H{
			"balance": wallet.Balance,
		},
	})
}
