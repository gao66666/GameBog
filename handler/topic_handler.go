package handler

import (
	"strconv"
	"strings"

	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
)

// DeleteTemporaryTopic 删除临时话题（仅限临时话题）
func (h *TopicHandler) DeleteTemporaryTopic(c *gin.Context) {
	topicID, ok := parseUintParam(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	err := h.se.DeleteTemporaryTopic(topicID)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "删除成功")
}

type TopicHandler struct {
	se *service.TopicService
}

func NewTopicHandler(serve *service.TopicService) *TopicHandler {
	return &TopicHandler{se: serve}
}

// GetTopicsPublic 获取可用话题列表（长期 + 未过期临时）。
func (h *TopicHandler) GetTopicsPublic(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "6"))
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 6
	}
	if size > 30 {
		size = 30
	}
	list, total, err := h.se.ListActiveTopicsPaged(page, size)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"topics": list, "total": total}, "ok")
}

// CreateTopicAuth 在话题页创建话题：自由输入，分长期/临时（临时 48h 过期）。
func (h *TopicHandler) CreateTopicAuth(c *gin.Context) {
	var body struct {
		Name        string `json:"name"`
		Kind        string `json:"kind"`         // "long" | "temporary"
		IsTemporary *bool  `json:"is_temporary"` // 兼容字段
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	name := strings.TrimSpace(body.Name)
	isTemp := false
	if body.IsTemporary != nil {
		isTemp = *body.IsTemporary
	} else if strings.EqualFold(strings.TrimSpace(body.Kind), "temporary") || strings.TrimSpace(body.Kind) == "临时" {
		isTemp = true
	}

	t, err := h.se.CreateTopic(name, isTemp)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}

	tool.ResponseSuccess(c, gin.H{"topic": t}, "ok")
}

func (h *TopicHandler) GetTopicPublic(c *gin.Context) {
	topicID, ok := parseUintParam(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	t, err := h.se.GetActiveTopic(topicID)
	if err != nil {
		tool.ResponseErrorWithMsg(c, "话题不存在或已过期")
		return
	}
	tool.ResponseSuccess(c, gin.H{"topic": t}, "ok")
}

func (h *TopicHandler) GetTopicArticlesPublic(c *gin.Context) {
	topicID, ok := parseUintParam(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "30"))
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 30
	}
	if size > 30 {
		size = 30
	}

	list, total, err := h.se.ListTopicArticles(topicID, page, size)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"article_list": list, "total": total}, "ok")
}

func (h *TopicHandler) GetTopicDiscussionsPublic(c *gin.Context) {
	topicID, ok := parseUintParam(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 50 {
		size = 50
	}

	list, total, err := h.se.ListDiscussions(topicID, page, size)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"list": list, "total": total}, "ok")
}

func (h *TopicHandler) CreateTopicDiscussionAuth(c *gin.Context) {
	topicID, ok := parseUintParam(c.Param("id"))
	if !ok {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	userID := c.GetUint64("userID")
	if userID == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	if err := h.se.CreateDiscussion(topicID, userID, body.Content); err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, nil, "ok")
}

func parseUintParam(s string) (uint, bool) {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint(n), true
}
