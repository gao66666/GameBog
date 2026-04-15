package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/search"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type SearchHandler struct {
	articleRepo *database.ArticleRepository
	userRepo    *database.UserRepository
	commentRepo *database.CommentRepository
}

func NewSearchHandler(articleRepo *database.ArticleRepository, userRepo *database.UserRepository, commentRepo *database.CommentRepository) *SearchHandler {
	return &SearchHandler{articleRepo: articleRepo, userRepo: userRepo, commentRepo: commentRepo}
}

type searchArticleResult struct {
	ID           uint64 `json:"id,string"`
	Title        string `json:"title"`
	Summary      string `json:"summary"`
	ViewCount    uint64 `json:"viewCount"`
	LikeCount    uint64 `json:"likeCount"`
	CommentCount int64  `json:"commentCount"`
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

	// 先查用户（总是用 MySQL），再查文章（ES 优先 -> MySQL 兜底）。
	var (
		userResults    []*models.User
		articleResults any
	)

	if h.userRepo != nil {
		if users, err := h.userRepo.SearchUsersByNameOrTel(query, 10); err == nil {
			userResults = users
		} else {
			zap.L().Warn("用户搜索失败", zap.Error(err))
		}
	}

	if !search.Enabled() {
		if h.articleRepo == nil {
			tool.ResponseSuccess(c, gin.H{"articles": []any{}, "users": userResults}, "ok")
			return
		}
		articles, err := h.articleRepo.SearchArticlesFallback(query, 20)
		if err != nil {
			zap.L().Error("MySQL 搜索降级查询失败", zap.Error(err))
			tool.ResponseSuccess(c, gin.H{"articles": []any{}, "users": userResults}, "ok")
			return
		}
		articleResults = articles
	} else {
		hits, err := search.SearchArticles(c.Request.Context(), query, 20)
		if err != nil {
			zap.L().Error("Elasticsearch 搜索失败", zap.Error(err))
			// ES 抖动时也可兜底 MySQL 简单查询
			if h.articleRepo != nil {
				articles, fbErr := h.articleRepo.SearchArticlesFallback(query, 20)
				if fbErr == nil {
					articleResults = articles
				} else {
					zap.L().Warn("MySQL 搜索兜底也失败", zap.Error(fbErr))
				}
			}
		} else {
			articleResults = hits
		}
	}

	if articleResults == nil {
		articleResults = []any{}
	}

	// 统一补齐统计字段（view/like/comment），方便前端展示。
	// - ES 命中仅有摘要字段，需要补齐 view/like/comment
	// - MySQL 兜底可直接读到 view/like，再补 comment
	articlesOut := make([]searchArticleResult, 0)
	articleIDs := make([]uint64, 0)

	switch v := articleResults.(type) {
	case []*models.Article:
		for _, a := range v {
			if a == nil || a.ID == 0 {
				continue
			}
			articlesOut = append(articlesOut, searchArticleResult{
				ID:        a.ID,
				Title:     a.Title,
				Summary:   a.Summary,
				ViewCount: a.ViewCount,
				LikeCount: a.LikeCount,
			})
			articleIDs = append(articleIDs, a.ID)
		}
	case []map[string]any:
		// ES 命中：先提取 id，再尝试用 MySQL 补齐 view/like/summary（若可用）
		ids := make([]uint64, 0, len(v))
		base := make(map[uint64]searchArticleResult, len(v))
		for _, m := range v {
			rawID, ok := m["id"]
			if !ok {
				continue
			}
			var id uint64
			switch t := rawID.(type) {
			case string:
				parsed, _ := strconv.ParseUint(t, 10, 64)
				id = parsed
			case float64:
				id = uint64(t)
			case int64:
				id = uint64(t)
			case uint64:
				id = t
			}
			if id == 0 {
				continue
			}
			ids = append(ids, id)
			base[id] = searchArticleResult{
				ID:      id,
				Title:   asString(m["title"]),
				Summary: asString(m["summary"]),
			}
		}

		// MySQL 补齐统计字段（若 repo 可用）
		if h.articleRepo != nil && len(ids) > 0 {
			var dbArticles []*models.Article
			if err := h.articleRepo.GetArticlesByIDs(ids, &dbArticles); err == nil {
				for _, a := range dbArticles {
					if a == nil || a.ID == 0 {
						continue
					}
					cur := base[a.ID]
					if cur.Title == "" {
						cur.Title = a.Title
					}
					if cur.Summary == "" {
						cur.Summary = a.Summary
					}
					cur.ViewCount = a.ViewCount
					cur.LikeCount = a.LikeCount
					base[a.ID] = cur
				}
			}
		}

		for _, id := range ids {
			articlesOut = append(articlesOut, base[id])
			articleIDs = append(articleIDs, id)
		}
	default:
		// 未知格式：不强行处理
	}

	// 批量补齐评论数
	if h.commentRepo != nil && len(articleIDs) > 0 {
		if cm, err := h.commentRepo.CountByArticleIDs(articleIDs); err == nil {
			for i := range articlesOut {
				if v, ok := cm[articlesOut[i].ID]; ok {
					articlesOut[i].CommentCount = v
				}
			}
		} else {
			zap.L().Warn("批量统计评论数失败", zap.Error(err))
		}
	}

	tool.ResponseSuccess(c, gin.H{
		"articles": articlesOut,
		"users":    userResults,
	}, "ok")
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		// ES JSON decode number -> float64
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return fmt.Sprintf("%v", t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	default:
		return fmt.Sprint(t)
	}
}
