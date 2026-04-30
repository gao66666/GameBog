package handler

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/jwt_module"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"go.uber.org/zap"
)

const anonSessionCookieName = "gb_sid"

const articleSummaryMaxRunes = 500


//限制最大标签数量和标签最大长度
func normalizeTagNames(in []string) ([]string, bool) {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		// 与 models.Tag 的 size:50 保持一致（按 rune 粗略限制）
		if len([]rune(t)) > 50 {
			return nil, false
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) > 5 {
			return nil, false
		}
	}
	return out, true
}

func ensureAnonSessionID(c *gin.Context) (string, error) {
	if sid, err := c.Cookie(anonSessionCookieName); err == nil {
		if sid != "" {
			return sid, nil
		}
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	sid := hex.EncodeToString(b)
	// 30 天
	c.SetCookie(anonSessionCookieName, sid, int((30 * 24 * time.Hour).Seconds()), "/", "", false, true)
	return sid, nil
}

// resolveActorKey 优先使用登录用户维度（更稳定），匿名用户回退到会话维度（cookie）。
func resolveActorKey(c *gin.Context) string {
	// 1) 兼容：若未来把阅读接口移入鉴权组，这里可以直接从 context 取 userID
	if uid, ok := c.Get("userID"); ok {
		if u, ok := uid.(uint64); ok && u != 0 {
			return "u:" + strconv.FormatUint(u, 10)
		}
	}

	// 2) 公共接口：尝试从 Authorization Bearer token 解析用户
	auth := c.GetHeader("Authorization")
	if auth != "" {
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if claims, err := jwt_module.ParseToken(parts[1]); err == nil && claims != nil && claims.UserID != 0 {
				return "u:" + strconv.FormatUint(claims.UserID, 10)
			}
		}
	}

	// 3) 匿名：按会话去重（避免 NAT/IP 误伤）
	if sid, err := ensureAnonSessionID(c); err == nil && sid != "" {
		return "s:" + sid
	}
	return "anonymous"
}

func (h *ArticleHandler) CreateArticleHandle(c *gin.Context) {
	// 1. 获取参数
	p := new(models.ParamPostArticle)
	if err := c.ShouldBindJSON(p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	summary := strings.TrimSpace(p.Summary)
	if summary == "" {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if len([]rune(summary)) > articleSummaryMaxRunes {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	tagNames, ok := normalizeTagNames(p.Tags)
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	// 2. 从 Context 中取中间件存入的 userID
	// 因为你在 middleware 里 c.Set("userID", claims.UserID)
	userID, exists := c.Get("userID")
	if !exists {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	// 3. 构造数据模型并交给 Service
	article := &models.Article{
		// 如果你用雪花算法，这里可以手动生成：ID: tool.GenID(),
		// 如果用自增ID，这里不写，GORM 会在 Create 之后自动填充到该结构体中
		ID:         tool.GenerateID(),
		Title:      p.Title,
		Content:    p.Content,
		Summary:    summary,
		AuthorID:   userID.(uint64),
		CategoryID: p.CategoryID, // 保持命名统一
		GameIDs:    p.GameIDs,
	}
	if len(tagNames) > 0 {
		article.Tags = make([]models.Tag, 0, len(tagNames))
		for _, n := range tagNames {
			article.Tags = append(article.Tags, models.Tag{Name: n})
		}
	}

	if err := h.se.CreateArticle(article); err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	zap.L().Info("创建文章成功")
	// 注意：雪花 ID（uint64）直接序列化为 JSON number 会在 JS 端丢失精度，
	// 统一返回字符串，前端可安全拼 URL/回填编辑态。
	tool.ResponseSuccess(c, gin.H{"article_id": strconv.FormatUint(article.ID, 10)}, "创建文章成功")
}

func (h *ArticleHandler) UpdateArticleHandle(c *gin.Context) {
	// 文章 ID 从路径获取
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	// 当前用户
	uid, ok := c.Get("userID")
	if !ok {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	userID := uid.(uint64)

	var body struct {
		Title      string   `json:"title"`
		Summary    string   `json:"summary"`
		Content    string   `json:"content"`
		Tags       []string `json:"tags"`
		GameIDs    []uint64 `json:"game_ids"`
		CategoryID uint     `json:"section_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Summary) == "" || strings.TrimSpace(body.Content) == "" {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if len([]rune(strings.TrimSpace(body.Summary))) > articleSummaryMaxRunes {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	tagNames, ok := normalizeTagNames(body.Tags)
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	if err := h.se.UpdateArticle(userID, id, body.Title, strings.TrimSpace(body.Summary), body.Content, tagNames, body.CategoryID, body.GameIDs); err != nil {
		tool.ResponseError(c, err)
		return
	}

	tool.ResponseSuccess(c, gin.H{"article_id": idStr}, "更新文章成功")
}

func (h *ArticleHandler) ReadArticleHandle(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		zap.L().Error("没有有效参数")
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	actorKey := resolveActorKey(c)
	article, err := h.se.ReadArticle(id, actorKey)
	if err != nil {
		zap.L().Error("GetArticleDetail failed", zap.Error(err))
		tool.ResponseErrorWithMsg(c, "文章不存在或已被删除")
		return
	}

	// Markdown 渲染（禁止原始 HTML，避免 XSS）
	var htmlBuf bytes.Buffer
	if err := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
		),
		goldmark.WithParserOptions(),
	).Convert([]byte(article.Content), &htmlBuf); err != nil {
		zap.L().Warn("render markdown failed", zap.Error(err))
	}

	tool.ResponseSuccess(c, gin.H{
		"article_id":   idStr,
		"title":        article.Title,
		"summary":      article.Summary,
		"content":      article.Content,
		"html_content": htmlBuf.String(),
		"view_count":   article.ViewCount,
		"like_count":   article.LikeCount,
		"author_id":    strconv.FormatUint(article.AuthorID, 10),
		"category_id":  article.CategoryID,
		"categoryId":   article.CategoryID,
		"created_at":   article.CreatedAt.Format("2006-01-02 15:04:05"),
		"tags":         article.Tags,
	}, "查询成功")
}

func (h *ArticleHandler) GetArticleListHandler(c *gin.Context) {

	authorIDStr := c.Query("author_id")
	pageStr := c.DefaultQuery("page", "1")  // 如果用户没传，默认第1页
	sizeStr := c.DefaultQuery("size", "10") // 如果用户没传，默认10条

	// 2. 转换类型 (这部分代码虽然枯燥，但必须写)
	authorID, _ := strconv.ParseUint(authorIDStr, 10, 64)
	page, _ := strconv.Atoi(pageStr)
	size, _ := strconv.Atoi(sizeStr)

	// 3. 剩下的逻辑和你之前写的一样

	list, total, err := h.se.GetArticleList(authorID, page, size)
	if err != nil {
		zap.L().Error("GetArticleList failed", zap.Error(err))
		tool.ResponseErrorWithMsg(c, "文章列表不存在或已被删除")
		return
	}
	tool.ResponseSuccess(c, gin.H{"article_list": list, "total": total}, "查询成功")

}

// GetLatestArticlesPublic 提供给首页使用的公开文章列表。
// - 首页“全部”：Redis ZSET（最多 18 条，3 页 * 6 条），为空回源 DB 并回填
// - 其他场景（带 author_id）：仍按 DB 创建时间倒序分页
func (h *ArticleHandler) GetLatestArticlesPublic(c *gin.Context) {
	authorIDStr := c.Query("author_id")
	pageStr := c.DefaultQuery("page", "1")
	sizeStr := c.DefaultQuery("size", "6")

	authorID, _ := strconv.ParseUint(authorIDStr, 10, 64)
	page, _ := strconv.Atoi(pageStr)
	size, _ := strconv.Atoi(sizeStr)
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 6
	}

	// 首页“全部”：不传 author_id（即 0）
	if authorID == 0 {
		list, total, err := h.se.GetLatestArticlesAll(page, size)
		if err != nil {
			zap.L().Error("GetLatestArticlesAll failed", zap.Error(err))
			tool.ResponseErrorWithMsg(c, "文章列表不可用")
			return
		}
		tool.ResponseSuccess(c, gin.H{"article_list": list, "total": total}, "查询成功")
		return
	}

	list, total, err := h.se.GetArticleList(authorID, page, size)
	if err != nil {
		zap.L().Error("GetLatestArticlesPublic failed", zap.Error(err))
		tool.ResponseErrorWithMsg(c, "文章列表不存在或已被删除")
		return
	}

	tool.ResponseSuccess(c, gin.H{"article_list": list, "total": total}, "查询成功")
}

// GetFollowingLatestArticles 仅关注：拉取关注用户近30天文章，并按阅读量倒序分页。
func (h *ArticleHandler) GetFollowingLatestArticles(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "1")
	sizeStr := c.DefaultQuery("size", "6")

	page, _ := strconv.Atoi(pageStr)
	size, _ := strconv.Atoi(sizeStr)
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 6
	}

	userID := c.GetUint64("userID")
	list, total, err := h.se.GetFollowingLatestArticlesByView(userID, page, size)
	if err != nil {
		zap.L().Error("GetFollowingLatestArticles failed", zap.Error(err))
		tool.ResponseErrorWithMsg(c, "文章列表不可用")
		return
	}
	tool.ResponseSuccess(c, gin.H{"article_list": list, "total": total}, "查询成功")
}

// GetArticleListPublic 公开文章列表（支持按作者筛选），用于个人中心“我的文章”分页等场景。
func (h *ArticleHandler) GetArticleListPublic(c *gin.Context) {
	authorIDStr := c.Query("author_id")
	pageStr := c.DefaultQuery("page", "1")
	sizeStr := c.DefaultQuery("size", "10")

	authorID, _ := strconv.ParseUint(authorIDStr, 10, 64)
	page, _ := strconv.Atoi(pageStr)
	size, _ := strconv.Atoi(sizeStr)
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}

	list, total, err := h.se.GetArticleList(authorID, page, size)
	if err != nil {
		zap.L().Error("GetArticleListPublic failed", zap.Error(err))
		tool.ResponseErrorWithMsg(c, "文章列表不存在或已被删除")
		return
	}
	tool.ResponseSuccess(c, gin.H{"article_list": list, "total": total}, "查询成功")
}

// GetArticleLeaderboardPublic 提供给首页使用的排行榜（默认阅读榜）。
// type=view|like
func (h *ArticleHandler) GetArticleLeaderboardPublic(c *gin.Context) {
	actionType := c.DefaultQuery("type", "view")
	if actionType != "view" && actionType != "like" {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	list, err := h.se.GetLeaderboard(actionType)
	if err != nil {
		zap.L().Error("GetArticleLeaderboardPublic failed", zap.Error(err))
		tool.ResponseErrorWithMsg(c, "排行榜暂不可用")
		return
	}

	tool.ResponseSuccess(c, gin.H{"type": actionType, "article_list": list}, "查询成功")
}
func (h *ArticleHandler) DeleteArticleHandle(c *gin.Context) {
	// 1. 获取文章 ID
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 64)

	// 2. 获取当前登录用户的 ID (从 JWT 中间件存入的值中取)
	// 假设你在中间件里用的 Key 是 "userID"
	currUserID, exists := c.Get("userID")
	if !exists {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	// 3. 传给 Service，让 Service 确认这个文章是不是这个人的
	if err := h.se.DeleteArticle(currUserID.(uint64), id); err != nil {
		tool.ResponseErrorWithMsg(c, "权限不足或删除失败")
		return
	}

	tool.ResponseSuccess(c, nil, "删除成功")
}

func (h *ArticleHandler) LikeArticleHandle(c *gin.Context) {
	// 兼容前端传字符串或数字形式的 article_id
	var raw struct {
		ArticleID interface{} `json:"article_id"`
		IsCancel  bool        `json:"is_cancel"` // false为点赞，true为取消点赞
	}

	if err := c.ShouldBindJSON(&raw); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	// 提取并解析 article_id
	var articleID uint64
	switch v := raw.ArticleID.(type) {
	case string:
		id, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil || id == 0 {
			tool.ResponseError(c, ErrCodeInvalidParam)
			return
		}
		articleID = id
	case float64:
		if v <= 0 {
			tool.ResponseError(c, ErrCodeInvalidParam)
			return
		}
		articleID = uint64(v)
	default:
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	userID, exists := c.Get("userID")
	if !exists {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	// 先写幂等key，再投递NSQ，消费者批量刷新MySQL
	err := h.se.LikeArticle(articleID, userID.(uint64), raw.IsCancel)
	if err != nil {
		zap.L().Error("点赞失败", zap.Error(err))
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	tool.ResponseSuccess(c, nil, "操作已接收")
}
