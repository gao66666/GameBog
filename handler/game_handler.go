package handler

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func encodeGameTags(tags []string) datatypes.JSON {
	if tags == nil {
		tags = []string{}
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return datatypes.JSON([]byte("[]"))
	}
	return datatypes.JSON(b)
}

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
	priceCents := int64(-1)
	if p.PriceCents != nil {
		priceCents = *p.PriceCents
	}
	game := &models.Game{
		ID:           tool.GenerateID(),
		Name:         p.Name,
		Description:  p.Description,
		ReleaseAt:    releaseAt,
		Publisher:    p.Publisher,
		Developer:    p.Developer,
		CoverURL:     strings.TrimSpace(p.CoverURL),
		PriceCents:   priceCents,
		Tags:         encodeGameTags(p.Tags),
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
	old.CoverURL = strings.TrimSpace(p.CoverURL)
	if p.PriceCents != nil {
		old.PriceCents = *p.PriceCents
	}
	old.Tags = encodeGameTags(p.Tags)
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
	userID := AuthUserID(c)
	detail, err := h.se.GetGameDetailForUser(gameID, userID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tool.ResponseErrorWithMsg(c, "游戏不存在")
			return
		}
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{
		"game":  detail.Game,
		"stock": detail.Stock,
		"owned": detail.Owned,
		"free":  detail.Free,
	}, "查询成功")
}

func (h *GameHandler) ListGames(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	list, total, err := h.se.ListGames(page, size)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	rows := make([]gin.H, 0, len(list))
	for _, g := range list {
		if g == nil {
			continue
		}
		stock, _ := h.se.StockForGame(g.ID, g.PriceCents)
		rows = append(rows, gin.H{
			"id":           strconv.FormatUint(g.ID, 10),
			"name":         g.Name,
			"description":  g.Description,
			"publisher":    g.Publisher,
			"developer":    g.Developer,
			"coverUrl":     g.CoverURL,
			"cover_url":    g.CoverURL,
			"priceCents":   g.PriceCents,
			"price_cents":  g.PriceCents,
			"free":         models.IsFreeGame(g.PriceCents),
			"stock":        stock,
			"tags":         g.Tags,
			"releaseAt":    g.ReleaseAt,
			"release_at":   g.ReleaseAt,
		})
	}
	tool.ResponseSuccess(c, gin.H{"list": rows, "total": total}, "查询成功")
}

func (h *GameHandler) PurchaseGame(c *gin.Context) {
	userID := AuthUserID(c)
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	gameID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	var req struct {
		IdempotencyKey string `json:"idempotency_key"`
	}
	_ = c.ShouldBindJSON(&req)
	idem := strings.TrimSpace(req.IdempotencyKey)
	if idem == "" {
		idem = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	order, err := h.se.PurchaseGame(userID, gameID, idem)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{
		"order":           order,
		"activation_code": order.Code,
	}, "购买成功")
}

func (h *GameHandler) ListMyGameOrders(c *gin.Context) {
	userID := AuthUserID(c)
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	list, err := h.se.ListMyGameOrders(userID)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	if list == nil {
		list = []models.GameOrder{}
	}
	tool.ResponseSuccess(c, gin.H{"orders": list}, "ok")
}

func (h *GameHandler) SearchGames(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "5"))
	list, err := h.se.SearchGamesByName(q, limit)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"games": list, "total": len(list)}, "查询成功")
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
	uid := c.GetUint64("userID")
	if review.UserID != uid {
		tool.ResponseErrorWithMsg(c, "只能修改自己的点评")
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
	rev, err := h.se.GetReviewByID(reviewID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tool.ResponseErrorWithMsg(c, "点评不存在")
			return
		}
		tool.ResponseError(c, err)
		return
	}
	if rev.UserID != c.GetUint64("userID") {
		tool.ResponseErrorWithMsg(c, "只能删除自己的点评")
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
	nameCache := make(map[uint64]string)
	out := make([]gin.H, 0, len(list))
	for _, r := range list {
		if r == nil {
			continue
		}
		uname := ""
		if h.userSe != nil {
			if n, ok := nameCache[r.UserID]; ok {
				uname = n
			} else {
				if u, err := h.userSe.GetUserBaseCached(r.UserID); err == nil && u != nil {
					uname = u.Name
				}
				nameCache[r.UserID] = uname
			}
		}
		out = append(out, gin.H{
			"id":         strconv.FormatUint(r.ID, 10),
			"gameId":     strconv.FormatUint(r.GameID, 10),
			"userId":     strconv.FormatUint(r.UserID, 10),
			"userName":   uname,
			"rating":     r.Rating,
			"content":    r.Content,
			"reviewedAt": r.ReviewedAt,
			"createdAt":  r.CreatedAt,
			"updatedAt":  r.UpdatedAt,
		})
	}
	tool.ResponseSuccess(c, gin.H{"list": out, "total": total}, "查询成功")
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
	if comment.UserID != c.GetUint64("userID") {
		tool.ResponseErrorWithMsg(c, "只能修改自己的回复")
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
	cm, err := h.se.GetReviewCommentByID(commentID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			tool.ResponseErrorWithMsg(c, "回复不存在")
			return
		}
		tool.ResponseError(c, err)
		return
	}
	if cm.UserID != c.GetUint64("userID") {
		tool.ResponseErrorWithMsg(c, "只能删除自己的回复")
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

// --- UserGamePlay ---

// UpsertMyGamePlay 新增/更新我的游戏记录
func (h *GameHandler) UpsertMyGamePlay(c *gin.Context) {
	var p models.ParamUpsertUserGamePlay
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	gameID, ok := parseUint64Param(p.GameID)
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	ugp := &models.UserGamePlay{
		UserID:         c.GetUint64("userID"),
		GameID:         gameID,
		PlaytimeTotal:  p.PlaytimeTotal,
		Playtime2Weeks: p.Playtime2Weeks,
	}
	if len(p.AchievedIDs) > 0 {
		raw, _ := json.Marshal(p.AchievedIDs)
		ugp.AchievedIDs = raw
	}

	if err := h.se.UpsertUserGamePlay(ugp); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "保存成功")
}

// GetMyGamePlays 获取我的游戏列表
func (h *GameHandler) GetMyGamePlays(c *gin.Context) {
	list, err := h.se.ListUserGamePlays(c.GetUint64("userID"))
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	if list == nil {
		list = []*models.UserGamePlay{}
	}
	tool.ResponseSuccess(c, gin.H{"list": list})
}

// GetUserGamePlays 获取公开的用户游戏列表
func (h *GameHandler) GetUserGamePlays(c *gin.Context) {
	userID, ok := parseUint64Param(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	list, err := h.se.ListUserGamePlays(userID)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	if list == nil {
		list = []*models.UserGamePlay{}
	}
	tool.ResponseSuccess(c, gin.H{"list": list})
}

// DeleteMyGamePlay 删除我的游戏记录
func (h *GameHandler) DeleteMyGamePlay(c *gin.Context) {
	gameID, ok := parseUint64Param(c.Param("gameId"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if err := h.se.DeleteUserGamePlay(c.GetUint64("userID"), gameID); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "删除成功")
}
