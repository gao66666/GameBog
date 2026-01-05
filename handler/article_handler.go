package handler

import (
	"net/http"
	"strconv"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var (
	ErrCodeInvalidParam = tool.NewBizError(404, 40001, "不正确的参数")
	ErrInvalidToken     = tool.NewBizError(404, 40001, "过期token")
	CodeServerBusy      = tool.NewBizError(404, 40001, "服务器繁忙")
)

func (h *ArticleHandler) CreateArticleHandle(c *gin.Context) {
	// 1. 获取参数
	p := new(models.ParamPostArticle)
	if err := c.ShouldBindJSON(p); err != nil {
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
		Summary:    p.Content, // 自动截取正文前100字作为摘要
		AuthorID:   userID.(uint64),
		CategoryID: p.CategoryID, // 保持命名统一
	}

	if err := h.se.CreateArticle(article); err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	zap.L().Info("创建文章成功")
}

func (h *ArticleHandler) ReadArticleHandle(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		zap.L().Error("没有有效参数")
	}
	if err != nil {
		zap.L().Error("GetArticleDetail failed", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"msg": "文章不存在或已被删除"})
		return
	}
	article, err := h.se.GetArticle(id)
	if err != nil {
		zap.L().Error("GetArticleDetail failed", zap.Error(err))
		c.JSON(http.StatusNotFound, gin.H{"msg": "文章不存在或已被删除"})
		return
	}
	//这里应该是将数据传给前端进行展示的
	c.JSON(http.StatusOK, gin.H{
		"msg": "查询成功",
		"data": gin.H{
			"title":      article.Title,
			"summary":    article.Summary,
			"content":    article.Content, // 你的“正文段落”
			"view_count": article.ViewCount,
			"author_id":  article.AuthorID,
			"created_at": article.CreatedAt.Format("2006-01-02 15:04:05"),
			"tags":       article.Tags,
		},
	})
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
		c.JSON(http.StatusNotFound, gin.H{"msg": "文章列表不存在或已被删除"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"msg": "查询成功",
		"data": gin.H{
			"article_list": list,
			"total":        total,
		},
	})

}
func (h *ArticleHandler) DeleteArticleHandle(c *gin.Context) {
	// 1. 获取文章 ID
	idStr := c.Param("id")
	id, _ := strconv.ParseUint(idStr, 10, 64)

	// 2. 获取当前登录用户的 ID (从 JWT 中间件存入的值中取)
	// 假设你在中间件里用的 Key 是 "userID"
	currUserID, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"msg": "未登录"})
		return
	}

	// 3. 传给 Service，让 Service 确认这个文章是不是这个人的
	if err := h.se.DeleteArticle(currUserID.(uint64), id); err != nil {
		c.JSON(http.StatusForbidden, gin.H{"msg": "权限不足或删除失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"msg": "删除成功"})
}
