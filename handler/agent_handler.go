package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/handler/agentstream"
	"github.com/gao66666/GoBlog/middleware"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// 与 Python memory.short_window_messages 对齐：网关侧列表最多保留条数（每条为一轮 user/assistant 之一）。
const agentChatMaxShortMessages = 12

// AgentHandler 通过 HTTP SSE 代理 Python Agent /chat，并管理网关侧短期对话。
// chatStore 为博客 Redis 中的短期对话；Agent 侧 Redis 可独立，仅用于其中/长期记忆等。
type AgentHandler struct {
	agentURL         string
	httpClient       *http.Client
	sem              chan struct{} // 并发控制信号量
	chatStore        *database.RedisAgentChatStore
	streamTranslator *agentstream.Translator
}

// NewAgentHandler 创建 AgentHandler。AGENT_URL 为空时仍返回非 nil，以便注册路由并返回明确错误。
func NewAgentHandler(chat *database.RedisAgentChatStore) *AgentHandler {
	agentURL := strings.TrimRight(os.Getenv("AGENT_URL"), "/")
	timeoutSec := getEnvInt("AGENT_TIMEOUT_SEC", 120)
	maxConc := getEnvInt("AGENT_MAX_CONCURRENCY", 50)
	if agentURL == "" {
		zap.L().Warn("AGENT_URL 未设置：Agent HTTP 代理将不可用，请配置 Python Agent 根地址（如 http://host.docker.internal:9091）")
	}
	tr, err := agentstream.NewTranslator()
	if err != nil {
		zap.L().Warn("tools_registry 加载失败，Agent SSE 将降级为透传", zap.Error(err))
	}
	return &AgentHandler{
		agentURL: agentURL,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
		sem:              make(chan struct{}, maxConc),
		chatStore:        chat,
		streamTranslator: tr,
	}
}

func getEnvInt(name string, def int) int {
	val := os.Getenv(name)
	if val == "" {
		return def
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return def
	}
	return n
}

// agentRequest 发给 Python Agent 的请求体
type agentRequest struct {
	Message               string              `json:"message"`
	UserID                uint64              `json:"user_id"`
	Token                 string              `json:"token"`
	ChatSessionID         string              `json:"chat_session_id"`
	RequestID             string              `json:"request_id,omitempty"`
	ConversationHistory   []map[string]string `json:"conversation_history,omitempty"`
	HistoryOwnedByGateway bool                `json:"history_owned_by_gateway"`
}

// chatProxyRequest 前端发来的请求体（user_id 以 JWT 为准，忽略 body 中的 user_id）
type chatProxyRequest struct {
	Message         string `json:"message"`
	Token           string `json:"token"`
	ChatSessionID   string `json:"chat_session_id"`
}

// ChatProxy 将 POST 代理到 Python Agent；短期对话由网关写入博客 Redis，请求中携带 conversation_history 供 Agent 组装上下文。
func (h *AgentHandler) ChatProxy(c *gin.Context) {
	if h == nil || h.agentURL == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": "Agent 未配置：请设置环境变量 AGENT_URL 为 Python Agent 服务根地址（例如 Docker 内 http://agent:8000，或宿主机 http://host.docker.internal:9091），并确保 Agent 进程已启动",
		})
		return
	}

	uid := c.GetUint64("userID")
	if uid == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}

	reqID := ""
	if v, ok := c.Get(middleware.RequestIDKey); ok {
		if s, ok2 := v.(string); ok2 {
			reqID = s
		}
	}
	if reqID == "" {
		reqID = c.Writer.Header().Get("X-Request-Id")
	}

	var req chatProxyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式错误"})
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "消息不能为空"})
		return
	}

	// 并发控制
	select {
	case h.sem <- struct{}{}:
		defer func() { <-h.sem }()
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "服务繁忙，请稍后再试"})
		return
	}

	start := time.Now()

	ctx, cancel := context.WithTimeout(c.Request.Context(), h.httpClient.Timeout)
	defer cancel()

	var chatSessionID string
	if h.chatStore != nil {
		sid := strings.TrimSpace(req.ChatSessionID)
		if sid == "" {
			var err error
			sid, err = h.chatStore.EnsureCurrentOrCreate(ctx, uid)
			if err != nil || sid == "" {
				zap.L().Warn("会话初始化失败", zap.Error(err))
				c.JSON(http.StatusInternalServerError, gin.H{"error": "会话初始化失败"})
				return
			}
		} else {
			if !h.chatStore.SessionExists(ctx, uid, sid) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "会话不存在或已删除"})
				return
			}
			if err := h.chatStore.SetCurrentSession(ctx, uid, sid); err != nil {
				zap.L().Warn("设置当前会话失败", zap.Error(err))
			}
		}
		chatSessionID = sid
	}

	zap.L().Info("agent chat proxy start",
		zap.Uint64("user_id", uid),
		zap.String("request_id", reqID),
		zap.Int("msg_len", len(req.Message)),
		zap.String("chat_session_id", chatSessionID),
	)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	chunkCount := 0
	runRes, err := h.RunAgentChat(ctx, AgentChatRunInput{
		UserID:        uid,
		Message:       req.Message,
		Token:         req.Token,
		ChatSessionID: chatSessionID,
		RequestID:     reqID,
	}, func(out string) error {
		_, werr := c.Writer.Write([]byte(out))
		if werr != nil {
			return werr
		}
		chunkCount++
		c.Writer.Flush()
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		zap.L().Warn("agent 服务不可用", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Agent 服务不可用"})
		return
	}

	zap.L().Info("agent chat proxy done",
		zap.Uint64("user_id", uid),
		zap.String("request_id", reqID),
		zap.Duration("latency", time.Since(start)),
		zap.Int("chunks", chunkCount),
		zap.String("turn_status", runRes.TurnRecord.Status),
		zap.Bool("persisted", !runRes.StreamErr && h.chatStore != nil && runRes.Assistant != ""),
	)
}

// HTTPClientTimeout 供 IM 等异步通道构造 context。
func (h *AgentHandler) HTTPClientTimeout() time.Duration {
	if h == nil || h.httpClient == nil {
		return 120 * time.Second
	}
	return h.httpClient.Timeout
}

// HistoryProxy 从博客 Redis 读取某会话消息；?session_id= 缺省时用当前会话。
func (h *AgentHandler) HistoryProxy(c *gin.Context) {
	uid := c.GetUint64("userID")
	if uid == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "48"))
	if limit < 1 {
		limit = 48
	}
	if limit > 100 {
		limit = 100
	}
	if h.chatStore == nil {
		tool.ResponseSuccess(c, gin.H{"messages": []gin.H{}}, "ok")
		return
	}
	ctx := c.Request.Context()
	sid := database.ParseSessionIDQuery(c.Query("session_id"))
	if sid == "" {
		var err error
		sid, err = h.chatStore.EnsureCurrentOrCreate(ctx, uid)
		if err != nil || sid == "" {
			zap.L().Warn("解析当前会话失败", zap.Error(err))
			tool.ResponseError(c, CodeServerBusy)
			return
		}
	} else {
		if !h.chatStore.SessionExists(ctx, uid, sid) {
			tool.ResponseError(c, ErrCodeInvalidParam)
			return
		}
		if err := h.chatStore.SetCurrentSession(ctx, uid, sid); err != nil {
			zap.L().Warn("切换当前会话失败", zap.Error(err))
		}
	}
	msgs, err := h.chatStore.ListMessages(ctx, uid, sid, limit)
	if err != nil {
		zap.L().Warn("读取网关对话历史失败", zap.Error(err))
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	out := make([]gin.H, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, gin.H{"role": m.Role, "content": m.Content})
	}
	tool.ResponseSuccess(c, gin.H{"messages": out, "session_id": sid}, "ok")
}

// ClearChatHistory 删除指定会话（或 query session_id 缺省时删当前会话），并同步 Python 该桶 short。
func (h *AgentHandler) ClearChatHistory(c *gin.Context) {
	uid := c.GetUint64("userID")
	if uid == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	ctx := c.Request.Context()
	sid := database.ParseSessionIDQuery(c.Query("session_id"))
	if sid == "" && h.chatStore != nil {
		var err error
		sid, err = h.chatStore.GetCurrentSessionID(ctx, uid)
		if err != nil {
			tool.ResponseError(c, CodeServerBusy)
			return
		}
	}
	if sid == "" {
		tool.ResponseSuccess(c, gin.H{"current_session_id": ""}, "ok")
		return
	}
	var newCurrent string
	if h.chatStore != nil {
		var err error
		newCurrent, err = h.chatStore.DeleteSession(ctx, uid, sid)
		if err != nil {
			zap.L().Warn("删除会话失败", zap.Error(err))
			tool.ResponseError(c, CodeServerBusy)
			return
		}
	}
	h.notifyPythonClearShort(ctx, uid, sid)
	tool.ResponseSuccess(c, gin.H{"current_session_id": newCurrent}, "ok")
}

// SessionsList 返回侧边栏历史会话及当前选中 id。
func (h *AgentHandler) SessionsList(c *gin.Context) {
	uid := c.GetUint64("userID")
	if uid == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	if h.chatStore == nil {
		tool.ResponseSuccess(c, gin.H{"sessions": []database.AgentSessionMeta{}, "current_session_id": ""}, "ok")
		return
	}
	ctx := c.Request.Context()
	sessions, err := h.chatStore.ListSessions(ctx, uid, 50)
	if err != nil {
		zap.L().Warn("列出会话失败", zap.Error(err))
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	if len(sessions) == 0 {
		if _, err := h.chatStore.CreateSession(ctx, uid); err != nil {
			zap.L().Warn("创建默认会话失败", zap.Error(err))
			tool.ResponseError(c, CodeServerBusy)
			return
		}
		sessions, err = h.chatStore.ListSessions(ctx, uid, 50)
		if err != nil {
			tool.ResponseError(c, CodeServerBusy)
			return
		}
	}
	current, err := h.chatStore.GetCurrentSessionID(ctx, uid)
	if err != nil {
		zap.L().Warn("读取当前会话失败", zap.Error(err))
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"sessions": sessions, "current_session_id": current}, "ok")
}

// CreateAgentSession POST 新建空会话并设为当前。
func (h *AgentHandler) CreateAgentSession(c *gin.Context) {
	uid := c.GetUint64("userID")
	if uid == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	if h.chatStore == nil {
		tool.ResponseSuccess(c, gin.H{"session_id": ""}, "ok")
		return
	}
	sid, err := h.chatStore.CreateSession(c.Request.Context(), uid)
	if err != nil {
		zap.L().Warn("创建会话失败", zap.Error(err))
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"session_id": sid}, "ok")
}

// DeleteAgentSession DELETE /agent/sessions/:id 删除一条历史会话。
func (h *AgentHandler) DeleteAgentSession(c *gin.Context) {
	uid := c.GetUint64("userID")
	if uid == 0 {
		tool.ResponseError(c, ErrInvalidToken)
		return
	}
	sid := database.SessionIDFromPath(c.Param("id"))
	if sid == "" {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	ctx := c.Request.Context()
	if h.chatStore != nil {
		if !h.chatStore.SessionExists(ctx, uid, sid) {
			tool.ResponseError(c, ErrCodeInvalidParam)
			return
		}
		newCurrent, err := h.chatStore.DeleteSession(ctx, uid, sid)
		if err != nil {
			zap.L().Warn("删除会话失败", zap.Error(err))
			tool.ResponseError(c, CodeServerBusy)
			return
		}
		h.notifyPythonClearShort(ctx, uid, sid)
		tool.ResponseSuccess(c, gin.H{"current_session_id": newCurrent}, "ok")
		return
	}
	h.notifyPythonClearShort(ctx, uid, sid)
	tool.ResponseSuccess(c, gin.H{"current_session_id": ""}, "ok")
}

func (h *AgentHandler) notifyPythonClearShort(ctx context.Context, uid uint64, sessionID string) {
	if h == nil || h.agentURL == "" {
		return
	}
	secret := strings.TrimSpace(os.Getenv("AGENT_INTERNAL_SECRET"))
	payload := map[string]interface{}{"user_id": uid}
	if strings.TrimSpace(sessionID) != "" {
		payload["session_id"] = strings.TrimSpace(sessionID)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.agentURL+"/memory/clear-short", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Agent-Secret", secret)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		zap.L().Warn("通知 Agent 清空本机 short 失败", zap.Error(err))
		return
	}
	_ = resp.Body.Close()
}
