package handler

import (
	"strconv"
	"strings"

	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
)

type PointsMallHandler struct {
	svc *service.PointsMallService
}

func NewPointsMallHandler(svc *service.PointsMallService) *PointsMallHandler {
	return &PointsMallHandler{svc: svc}
}

func (h *PointsMallHandler) ListProducts(c *gin.Context) {
	if h.svc == nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	list, err := h.svc.ListProducts()
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"products": list}, "ok")
}

func (h *PointsMallHandler) SearchProducts(c *gin.Context) {
	if h.svc == nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "5"))
	list, err := h.svc.SearchProductsByName(q, limit)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"products": list, "total": len(list)}, "ok")
}

func (h *PointsMallHandler) GetProduct(c *gin.Context) {
	if h.svc == nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	productID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	product, err := h.svc.GetProduct(productID)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"product": product}, "ok")
}

func (h *PointsMallHandler) ListMyOrders(c *gin.Context) {
	userID := c.GetUint64("userID")
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	list, err := h.svc.ListMyOrders(userID)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"orders": list}, "ok")
}

func (h *PointsMallHandler) Redeem(c *gin.Context) {
	userID := c.GetUint64("userID")
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	var req struct {
		ProductID      interface{} `json:"product_id"`
		IdempotencyKey string      `json:"idempotency_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	productID, ok := parseJSONUint64(req.ProductID)
	if !ok || productID == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	idem := strings.TrimSpace(req.IdempotencyKey)
	if idem == "" {
		idem = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}

	order, err := h.svc.Redeem(userID, productID, idem)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}

	tool.ResponseSuccess(c, gin.H{
		"order": order,
		"activation_code": order.Code,
	}, "兑换成功")
}

func parseJSONUint64(v interface{}) (uint64, bool) {
	switch x := v.(type) {
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(x), 10, 64)
		return n, err == nil
	case float64:
		if x <= 0 {
			return 0, false
		}
		return uint64(x), true
	default:
		return 0, false
	}
}
