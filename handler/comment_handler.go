package handler

import (
	"strconv"
	"strings"

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

	// 1. 获取 UID（从中间件拿到）
	userID := c.GetUint64("userID")

	// 2. 预处理父评论逻辑（为了拿到 RootID 和 ReplyUserID）
	// 这部分逻辑建议也封装在 Service 的某个辅助方法里，这里先保持清晰
	var rootID, replyUserID uint64
	if p.ParentID != 0 {
		if parent, err := h.se.GetCommentByID(p.ParentID); err == nil {
			replyUserID = parent.UserID
			rootID = parent.ID
			if parent.RootID != 0 {
				rootID = parent.RootID
			}
		}
	}

	// 3. 构造并调用 Service
	comment := &models.Comment{
		ID: tool.GenerateID(), UserID: userID, ArticleID: p.ArticleID,
		ParentID: p.ParentID, Content: p.Content, RootID: rootID, ReplyUserID: replyUserID,
	}

	saved, err := h.se.CreateComment(comment)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	// 4. 返回（格式化 ID 防止前端精度丢失）
	tool.ResponseSuccess(c, gin.H{"commentId": strconv.FormatUint(saved.ID, 10), "content": saved.Content}, "发表成功")
}

func (h *CommentHandler) GetArticleComment(c *gin.Context) {
	// 1. 获取并校验参数
	articleIDStr := c.Query("article_id")
	aid, err := strconv.ParseUint(articleIDStr, 10, 64)
	if err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	// 分页参数，给好默认值
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "10"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "2")) // 每层楼默认预览2条

	// 2. 调用服务层
	comments, total, err := h.se.GetCommentByArticleID(aid, page, size, limit)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	tool.ResponseSuccess(c, gin.H{"list": comments, "total": total})
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
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	tool.ResponseSuccess(c, gin.H{"total": total, "list": list})
}

// DeleteComment 删除评论（仅允许删除自己的评论）。
func (h *CommentHandler) DeleteComment(c *gin.Context) {
	commentID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || commentID == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	userID := c.GetUint64("userID")
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	if err := h.se.DeleteComment(userID, commentID); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"commentId": strconv.FormatUint(commentID, 10)}, "删除成功")
}

// LikeCommentHandle 点赞/取消点赞评论
func (h *CommentHandler) LikeCommentHandle(c *gin.Context) {
	var raw struct {
		CommentID interface{} `json:"comment_id"`
		IsCancel  bool        `json:"is_cancel"`
	}
	if err := c.ShouldBindJSON(&raw); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	var commentID uint64
	switch v := raw.CommentID.(type) {
	case string:
		id, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil || id == 0 {
			tool.ResponseError(c, ErrCodeInvalidParam)
			return
		}
		commentID = id
	case float64:
		if v <= 0 {
			tool.ResponseError(c, ErrCodeInvalidParam)
			return
		}
		commentID = uint64(v)
	default:
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	userID, exists := c.Get("userID")
	if !exists {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	if err := h.se.LikeComment(commentID, userID.(uint64), raw.IsCancel); err != nil {
		zap.L().Error("评论点赞失败", zap.Error(err))
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	tool.ResponseSuccess(c, nil, "操作已接收")
}

// CreateCYHandle 创建 CY 评论
func (h *CommentHandler) CreateCYHandle(c *gin.Context) {
	p := new(models.ParamCreateCY)
	if err := c.ShouldBindJSON(p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	comment := &models.Comment{
		ID:        tool.GenerateID(),
		UserID:    c.GetUint64("userID"),
		ArticleID: p.ArticleID,
		Content:   p.Content,
	}

	saved, err := h.se.CreateCY(comment)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}

	tool.ResponseSuccess(c, gin.H{"commentId": strconv.FormatUint(saved.ID, 10)}, "CY 成功")
}

// GetMyCYListHandle 获取当前用户的 CY 列表
func (h *CommentHandler) GetMyCYListHandle(c *gin.Context) {
	userID := c.GetUint64("userID")
	list, err := h.se.ListMyCYForDisplay(userID)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"list": list})
}
