package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// InternalHandler 供后台脚本使用的内部 API（不经前端、不走 JWT）。
type InternalHandler struct {
	gameSvc   *service.GameService
	topicSvc  *service.TopicService
	agentURL  string
	ingestKey string
	client    *http.Client
}

func NewInternalHandler(gameSvc *service.GameService, topicSvc *service.TopicService) *InternalHandler {
	return &InternalHandler{
		gameSvc:   gameSvc,
		topicSvc:  topicSvc,
		agentURL:  strings.TrimRight(os.Getenv("AGENT_URL"), "/"),
		ingestKey: strings.TrimSpace(os.Getenv("DOCUMENT_INGEST_SECRET")),
		client: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// CreateGameInternal 上新游戏并自动创建/绑定「游戏:名称」话题。
func (h *InternalHandler) CreateGameInternal(c *gin.Context) {
	var p models.ParamCreateGame
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	game, err := buildGameFromCreateParam(&p)
	if err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	topicID, err := h.gameSvc.CreateGameWithTopic(game)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{
		"game_id":  strconv.FormatUint(game.ID, 10),
		"topic_id": topicID,
	}, "创建成功")
}

// CreateTopicInternal 创建长期话题（非临时），用于后台上新。
func (h *InternalHandler) CreateTopicInternal(c *gin.Context) {
	var body struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	t, err := h.topicSvc.CreateTopic(strings.TrimSpace(body.Name), false)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, gin.H{"topic_id": t.ID, "name": t.Name}, "创建成功")
}

type internalKBMarkdownRequest struct {
	ArticleID       string   `json:"article_id" binding:"required"`
	Markdown        string   `json:"markdown" binding:"required"`
	Source          string   `json:"source"`
	GameName        string   `json:"game_name"`
	ContentRevision int      `json:"content_revision"`
	ChunkSize       int      `json:"chunk_size"`
	ChunkOverlap    int      `json:"chunk_overlap"`
	SectionOrder    []string `json:"section_order"`
}

// IngestKBMarkdown 将 Markdown 分片写入 Agent 公共 Qdrant 知识库（代理 Python /memory/ingest-markdown）。
func (h *InternalHandler) IngestKBMarkdown(c *gin.Context) {
	if h.agentURL == "" {
		tool.ResponseErrorWithMsg(c, "Agent 未配置：请设置 AGENT_URL")
		return
	}
	var req internalKBMarkdownRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	req.ArticleID = strings.TrimSpace(req.ArticleID)
	req.Markdown = strings.TrimSpace(req.Markdown)
	if req.ArticleID == "" || req.Markdown == "" {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if strings.TrimSpace(req.Source) == "" {
		req.Source = "backstage"
	}
	if req.ContentRevision < 1 {
		req.ContentRevision = 1
	}

	payload, _ := json.Marshal(map[string]any{
		"article_id":       req.ArticleID,
		"markdown":         req.Markdown,
		"source":           req.Source,
		"game_name":        strings.TrimSpace(req.GameName),
		"content_revision": req.ContentRevision,
		"chunk_size":       req.ChunkSize,
		"chunk_overlap":    req.ChunkOverlap,
		"section_order":    req.SectionOrder,
	})

	httpReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		http.MethodPost,
		h.agentURL+"/memory/ingest-markdown",
		bytes.NewReader(payload),
	)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if h.ingestKey != "" {
		httpReq.Header.Set("X-Ingest-Key", h.ingestKey)
	}

	resp, err := h.client.Do(httpReq)
	if err != nil {
		zap.L().Warn("kb markdown ingest proxy failed", zap.Error(err))
		tool.ResponseErrorWithMsg(c, "调用 Agent 入库失败")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		zap.L().Warn("agent ingest-markdown error", zap.Int("status", resp.StatusCode), zap.ByteString("body", body))
		tool.ResponseErrorWithMsg(c, "Agent 入库失败: "+string(body))
		return
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		tool.ResponseSuccess(c, gin.H{"raw": string(body)}, "ok")
		return
	}
	tool.ResponseSuccess(c, out, "ok")
}

func buildGameFromCreateParam(p *models.ParamCreateGame) (*models.Game, error) {
	releaseAt, err := parseDate(p.ReleaseAt)
	if err != nil {
		return nil, err
	}
	priceCents := int64(-1)
	if p.PriceCents != nil {
		priceCents = *p.PriceCents
	}
	return &models.Game{
		ID:          tool.GenerateID(),
		Name:        p.Name,
		Description: p.Description,
		ReleaseAt:   releaseAt,
		Publisher:   p.Publisher,
		Developer:   p.Developer,
		CoverURL:    strings.TrimSpace(p.CoverURL),
		PriceCents:  priceCents,
		Tags:        encodeGameTags(p.Tags),
	}, nil
}
