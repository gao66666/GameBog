package handler

import (
	"net/http"
	"strconv"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *CommentHandler) CreateComment(c *gin.Context) {
	p := new(models.ParamComment)
	if err := c.ShouldBindJSON(p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	// 1. 获取当前登录用户 ID
	val, exists := c.Get("userID")
	if !exists {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	currentUserID := val.(uint64)

	var rootID uint64
	var replyUserID uint64

	// 2. 逻辑推导
	if p.ParentID != 0 {
		parent, err := h.se.GetCommentByID(p.ParentID)
		if err != nil {
			zap.L().Warn("父评论不存在", zap.Uint64("parentID", p.ParentID))
			tool.ResponseError(c, CodeServerBusy)
			return
		}

		if parent.RootID == 0 {
			rootID = parent.ID
		} else {
			rootID = parent.RootID
		}
		replyUserID = parent.UserID
	}

	// 3. 构造存储对象
	newComment := &models.Comment{
		ID:          tool.GenerateID(),
		UserID:      currentUserID,
		ArticleID:   p.ArticleID,
		ParentID:    p.ParentID,
		Content:     p.Content,
		RootID:      rootID,
		ReplyUserID: replyUserID,
	}

	// 4. 调用 Service
	savedComment, err := h.se.CreateComment(newComment)
	if err != nil {
		zap.L().Error("插入评论失败", zap.Error(err))
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	zap.L().Info("插入评论成功", zap.Uint64("id", savedComment.ID))

	// 5. 返回结果
	c.JSON(http.StatusOK, gin.H{
		"msg": "发表成功",
		"data": gin.H{
			"commentId":   strconv.FormatUint(savedComment.ID, 10),
			"content":     savedComment.Content,
			"authorId":    strconv.FormatUint(savedComment.UserID, 10),
			"replyUserId": strconv.FormatUint(savedComment.ReplyUserID, 10),
			"createdAt":   savedComment.CreatedAt.Format("2006-01-02 15:04:05"),
		},
	})
}

func (h *CommentHandler) GetArticleComment(c *gin.Context) {
	// 1. 获取并校验参数
	articleIDStr := c.Query("article_id")
	aid, err := strconv.ParseUint(articleIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的文章ID"})
		return
	}

	// 分页参数，给好默认值
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "2")) // 每层楼默认预览2条

	// 2. 调用服务层
	comments, err := h.se.GetCommentByArticleID(aid, page, size, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取评论失败"})
		return
	}

	// 3. 返回结果
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": comments,
		"msg":  "success",
	})
}

func (h *CommentHandler) GetCommentDetail(c *gin.Context) {
	// 1. 获取路径里的 root_id
	rootID, _ := strconv.ParseUint(c.Param("root_id"), 10, 64)

	// 2. 获取分页参数并计算 offset
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	offset := (page - 1) * size

	// 3. 直接调用你刚才写的那个 Repo 方法 (或者通过 Service 转发)
	list, total, err := h.se.GetCommentFloorDetails(rootID, offset, size)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "查询详情失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"total": total,
			"list":  list,
		},
	})
}
