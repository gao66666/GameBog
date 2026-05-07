package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"github.com/olahol/melody"
	"go.uber.org/zap"
)

// 与 Python memory.short_window_messages 对齐：网关侧列表最多保留条数（每条为一轮 user/assistant 之一）。
const agentChatMaxShortMessages = 12

// AgentHandler 负责将 WS 消息转发到 Python Agent 服务并流式返回结果。
// 本 Handler 不依赖任何 Service 层，通过 HTTP 与 Agent 进程通信。
// chatStore 为博客 Redis 中的短期对话；Agent 侧 Redis 可独立，仅用于其中/长期记忆等。
type AgentHandler struct {
	agentURL   string
	httpClient *http.Client
	sem        chan struct{} // 并发控制信号量
	chatStore  *database.RedisAgentChatStore
}

// NewAgentHandler 创建 AgentHandler。AGENT_URL 为空时仍返回非 nil，以便注册路由并返回明确错误。
func NewAgentHandler(chat *database.RedisAgentChatStore) *AgentHandler {
	agentURL := strings.TrimRight(os.Getenv("AGENT_URL"), "/")
	timeoutSec := getEnvInt("AGENT_TIMEOUT_SEC", 120)
	maxConc := getEnvInt("AGENT_MAX_CONCURRENCY", 50)
	if agentURL == "" {
		zap.L().Warn("AGENT_URL 未设置：Agent HTTP 代理与 WS 转发将不可用，请配置 Python Agent 根地址（如 http://host.docker.internal:9091）")
	}
	return &AgentHandler{
		agentURL: agentURL,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
		sem:       make(chan struct{}, maxConc),
		chatStore: chat,
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
	ConversationHistory   []map[string]string `json:"conversation_history,omitempty"`
	HistoryOwnedByGateway bool                `json:"history_owned_by_gateway"`
}

// wsAgentEnvelope 前端 WS 发来的 agent_chat 消息格式
type wsAgentEnvelope struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Message string `json:"message"`
}

// HandleMessage 由 melody HandleMessage 回调触发，路由 agent_chat 消息。
func (h *AgentHandler) HandleMessage(s *melody.Session, msg []byte) {
	if h == nil {
		return
	}

	var env wsAgentEnvelope
	if err := json.Unmarshal(msg, &env); err != nil {
		return
	}
	if env.Type != "agent_chat" || env.Message == "" {
		return
	}
	if h.agentURL == "" {
		h.writeError(s, env.ID, "Agent 未配置：请为 Go 服务设置环境变量 AGENT_URL（Python Agent 服务地址）")
		return
	}

	// 从 WS Session 中取 userID 和 token（HandleWS 建连时存入）
	uid, _ := s.Get("userID")
	userID, _ := uid.(uint64)
	if userID == 0 {
		h.writeError(s, env.ID, "未登录")
		return
	}
	tok, _ := s.Get("token")
	token, _ := tok.(string)

	// 信号量并发控制
	select {
	case h.sem <- struct{}{}:
		go func() {
			defer func() { <-h.sem }()
			h.streamChat(s, env.ID, userID, token, env.Message)
		}()
	default:
		h.writeError(s, env.ID, "服务繁忙，请稍后再试")
	}
}

func (h *AgentHandler) streamChat(s *melody.Session, reqID string, userID uint64, token, message string) {
	start := time.Now()
	zap.L().Info("agent chat start",
		zap.Uint64("user_id", userID),
		zap.String("req_id", reqID),
		zap.Int("msg_len", len(message)),
	)

	ctx, cancel := context.WithTimeout(context.Background(), h.httpClient.Timeout)
	defer cancel()

	body, _ := json.Marshal(agentRequest{
		Message: message,
		UserID:  userID,
		Token:   token,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", h.agentURL+"/chat", bytes.NewReader(body))
	if err != nil {
		h.writeError(s, reqID, "请求构建失败")
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		zap.L().Warn("agent 服务不可用", zap.Error(err))
		h.writeError(s, reqID, "Agent 服务繁忙，请稍后再试")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		zap.L().Warn("agent 返回非 200", zap.Int("status", resp.StatusCode))
		h.writeError(s, reqID, fmt.Sprintf("Agent 异常 (HTTP %d)", resp.StatusCode))
		return
	}

	// 流式读取 Agent 的 SSE 响应，逐行透传给前端
	chunkCount := 0
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if s.IsClosed() {
			cancel()
			break
		}
		line := scanner.Text()

		// SSE 格式: "data: {...}\n\n"，空行是事件分隔符，跳过
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := line[6:] // 去掉 "data: " 前缀

		// 透传：直接包一层 WS 消息发给前端
		wsMsg, err := json.Marshal(wsAgentResponse{
			Type: "agent_chat",
			ID:   reqID,
			Data: json.RawMessage(payload),
		})
		if err != nil {
			continue
		}

		if err := s.Write(wsMsg); err != nil {
			zap.L().Warn("WS 写入失败，停止推送",
				zap.Uint64("user_id", userID),
				zap.Error(err),
			)
			cancel()
			return
		}
		chunkCount++
	}

	if err := scanner.Err(); err != nil {
		zap.L().Warn("读取 Agent SSE 流出错", zap.Error(err))
	}

	zap.L().Info("agent chat done",
		zap.Uint64("user_id", userID),
		zap.String("req_id", reqID),
		zap.Duration("latency", time.Since(start)),
		zap.Int("chunks", chunkCount),
	)
}

// wsAgentResponse 透传给前端的 WS 消息
type wsAgentResponse struct {
	Type string          `json:"type"`
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

// writeError 向 WS 写入错误消息
func (h *AgentHandler) writeError(s *melody.Session, reqID, errMsg string) {
	msg, _ := json.Marshal(map[string]interface{}{
		"type":  "agent_chat",
		"id":    reqID,
		"error": errMsg,
		"done":  true,
	})
	if err := s.Write(msg); err != nil {
		zap.L().Warn("WS 错误消息写入失败", zap.Error(err))
	}
}

// ============================================================
// HTTP SSE Proxy — 前端直接 POST 到 Go 后端，透传到 Python Agent
// ============================================================

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
		zap.Int("msg_len", len(req.Message)),
		zap.String("chat_session_id", chatSessionID),
	)

	ar := agentRequest{
		Message:               req.Message,
		UserID:                uid,
		Token:                 req.Token,
		ChatSessionID:         chatSessionID,
		HistoryOwnedByGateway: h.chatStore != nil,
	}
	if h.chatStore != nil && chatSessionID != "" {
		prev, err := h.chatStore.ListMessages(ctx, uid, chatSessionID, agentChatMaxShortMessages)
		if err != nil {
			zap.L().Warn("读取网关对话缓存失败", zap.Error(err))
		} else if len(prev) > 0 {
			ar.ConversationHistory = make([]map[string]string, 0, len(prev))
			for _, m := range prev {
				ar.ConversationHistory = append(ar.ConversationHistory, map[string]string{
					"role":    m.Role,
					"content": m.Content,
				})
			}
		}
	}

	body, err := json.Marshal(ar)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "请求序列化失败"})
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", h.agentURL+"/chat", bytes.NewReader(body))
	if err != nil {
		zap.L().Warn("agent 请求构建失败", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "请求构建失败"})
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		zap.L().Warn("agent 服务不可用", zap.Error(err))
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Agent 服务不可用"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		zap.L().Warn("agent 返回非 200", zap.Int("status", resp.StatusCode))
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("Agent 异常 (HTTP %d)", resp.StatusCode)})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	var tokenBuf strings.Builder
	streamErr := false
	chunkCount := 0
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			_, werr := c.Writer.Write([]byte("\n"))
			if werr != nil {
				return
			}
			c.Writer.Flush()
			continue
		}

		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "data: ") {
			var ev struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			if json.Unmarshal([]byte(trim[6:]), &ev) == nil {
				switch ev.Type {
				case "token":
					tokenBuf.WriteString(ev.Content)
				case "error":
					streamErr = true
				}
			}
		}

		_, werr := c.Writer.Write([]byte(line + "\n"))
		if werr != nil {
			return
		}
		c.Writer.Flush()
		chunkCount++
	}

	if err := scanner.Err(); err != nil {
		zap.L().Warn("读取 Agent SSE 流出错", zap.Error(err))
		streamErr = true
	}

	if !streamErr && ctx.Err() == nil && h.chatStore != nil && chatSessionID != "" {
		assistant := strings.TrimSpace(tokenBuf.String())
		if assistant != "" {
			ttl := 60 * time.Minute
			_ = h.chatStore.AppendMessages(ctx, uid, chatSessionID, []database.AgentChatMessage{
				{Role: "user", Content: req.Message},
				{Role: "assistant", Content: assistant},
			}, ttl, agentChatMaxShortMessages)
		}
	}

	zap.L().Info("agent chat proxy done",
		zap.Uint64("user_id", uid),
		zap.Duration("latency", time.Since(start)),
		zap.Int("chunks", chunkCount),
		zap.Bool("persisted", !streamErr && h.chatStore != nil && strings.TrimSpace(tokenBuf.String()) != ""),
	)
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
