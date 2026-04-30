package handler

import (
	"strconv"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func parseUint64Param(s string) (uint64, bool) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil || v == 0 {
		return 0, false
	}
	return v, true
}

func parseDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", dateStr)
}

func parseDateTime(dateStr string) (*time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
		return &t, nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05", dateStr); err == nil {
		return &t, nil
	}
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (h *GameHandler) CreateGame(c *gin.Context) {
	var p models.ParamCreateGame
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	releaseAt, err := parseDate(p.ReleaseAt)
	if err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	game := &models.Game{
		ID:          tool.GenerateID(),
		Name:        p.Name,
		Description: p.Description,
		ReleaseAt:   releaseAt,
		Publisher:   p.Publisher,
		Developer:   p.Developer,
	}
	if err := h.se.CreateGame(game); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"game_id": strconv.FormatUint(game.ID, 10)}, "创建成功")
}

func (h *GameHandler) UpdateGame(c *gin.Context) {
	gameID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	var p models.ParamUpdateGame
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	old, err := h.se.GetGameByID(gameID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tool.ResponseErrorWithMsg(c, "游戏不存在")
			return
		}
		tool.ResponseError(c, err)
		return
	}
	releaseAt, err := parseDate(p.ReleaseAt)
	if err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	old.Name = p.Name
	old.Description = p.Description
	old.ReleaseAt = releaseAt
	old.Publisher = p.Publisher
	old.Developer = p.Developer
	if err := h.se.UpdateGame(old); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"game_id": strconv.FormatUint(gameID, 10)}, "更新成功")
}

func (h *GameHandler) DeleteGame(c *gin.Context) {
	gameID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if err := h.se.DeleteGame(gameID); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "删除成功")
}

func (h *GameHandler) GetGame(c *gin.Context) {
	gameID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	game, err := h.se.GetGameByID(gameID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tool.ResponseErrorWithMsg(c, "游戏不存在")
			return
		}
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"game": game}, "查询成功")
}

func (h *GameHandler) ListGames(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	list, total, err := h.se.ListGames(page, size)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"list": list, "total": total}, "查询成功")
}

func (h *GameHandler) CreateReview(c *gin.Context) {
	gameID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	var p models.ParamCreateGameReview
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	review := &models.GameReview{
		ID:         tool.GenerateID(),
		GameID:     gameID,
		UserID:     c.GetUint64("userID"),
		ReviewedAt: time.Now(),
		Rating:     p.Rating,
		Content:    p.Content,
	}
	if err := h.se.CreateReview(review); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"review_id": strconv.FormatUint(review.ID, 10)}, "创建成功")
}

func (h *GameHandler) UpdateReview(c *gin.Context) {
	reviewID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	var p models.ParamUpdateGameReview
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	review, err := h.se.GetReviewByID(reviewID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tool.ResponseErrorWithMsg(c, "点评不存在")
			return
		}
		tool.ResponseError(c, err)
		return
	}
	review.Rating = p.Rating
	review.Content = p.Content
	review.ReviewedAt = time.Now()
	if err := h.se.UpdateReview(review); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"review_id": strconv.FormatUint(review.ID, 10)}, "更新成功")
}

func (h *GameHandler) DeleteReview(c *gin.Context) {
	reviewID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if err := h.se.DeleteReview(reviewID); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "删除成功")
}

func (h *GameHandler) GetReview(c *gin.Context) {
	reviewID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	review, err := h.se.GetReviewByID(reviewID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tool.ResponseErrorWithMsg(c, "点评不存在")
			return
		}
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"review": review}, "查询成功")
}

func (h *GameHandler) ListReviews(c *gin.Context) {
	gameID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	list, total, err := h.se.ListReviewsByGameID(gameID, page, size)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"list": list, "total": total}, "查询成功")
}

func (h *GameHandler) CreateReviewComment(c *gin.Context) {
	reviewID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	var p models.ParamCreateGameReviewComment
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	comment := &models.GameReviewComment{
		ID:       tool.GenerateID(),
		ReviewID: reviewID,
		UserID:   c.GetUint64("userID"),
		ParentID: p.ParentID,
		Content:  p.Content,
	}
	if err := h.se.CreateReviewComment(comment); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"comment_id": strconv.FormatUint(comment.ID, 10)}, "创建成功")
}

func (h *GameHandler) UpdateReviewComment(c *gin.Context) {
	commentID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	var p models.ParamUpdateGameReviewComment
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	comment, err := h.se.GetReviewCommentByID(commentID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tool.ResponseErrorWithMsg(c, "点评评论不存在")
			return
		}
		tool.ResponseError(c, err)
		return
	}
	comment.Content = p.Content
	if err := h.se.UpdateReviewComment(comment); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"comment_id": strconv.FormatUint(comment.ID, 10)}, "更新成功")
}

func (h *GameHandler) DeleteReviewComment(c *gin.Context) {
	commentID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if err := h.se.DeleteReviewComment(commentID); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "删除成功")
}

func (h *GameHandler) ListReviewComments(c *gin.Context) {
	reviewID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.se.ListReviewComments(reviewID, page, size)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"list": list, "total": total}, "查询成功")
}
	tool.ResponseSuccess(c, gin.H{"list": list, "total": total}, "查询成功")
}
