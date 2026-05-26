package handler

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/search"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type SearchHandler struct {
	se *service.SearchService
}

func NewSearchHandler(se *service.SearchService) *SearchHandler {
	return &SearchHandler{se: se}
}

func (h *SearchHandler) ProcessSearchSyncMessage(ctx context.Context, payload []byte) error {
	if !search.Enabled() {
		zap.L().Debug("Search disabled, skip sync")
		return nil
	}

	var p models.SearchSyncPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return err
	}

	return search.UpsertArticle(ctx, p)
}

func (h *SearchHandler) GlobalSearch(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))

	result, err := h.se.GlobalSearch(c.Request.Context(), query, page, size)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	tool.ResponseSuccess(c, gin.H{
		"articles": result.Articles,
		"users":    result.Users,
	}, "ok")
}
