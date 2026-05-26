package channel

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/handler"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Hub 统一收发层：注册适配器、处理 webhook、调 Agent、出站回复。
type Hub struct {
	agent   *handler.AgentHandler
	chat    *database.RedisAgentChatStore
	store   *database.ChannelStore
	adapters map[string]Adapter
}

func NewHub(agent *handler.AgentHandler, chat *database.RedisAgentChatStore, store *database.ChannelStore) *Hub {
	return &Hub{
		agent:    agent,
		chat:     chat,
		store:    store,
		adapters: make(map[string]Adapter),
	}
}

// Register 注册已启用的通道适配器。
func (h *Hub) Register(a Adapter) {
	if h == nil || a == nil || !a.Enabled() {
		return
	}
	name := strings.TrimSpace(a.Name())
	if name == "" {
		return
	}
	h.adapters[name] = a
	zap.L().Info("channel adapter registered", zap.String("channel", name))
}

func (h *Hub) adapter(name string) (Adapter, bool) {
	a, ok := h.adapters[strings.TrimSpace(name)]
	return a, ok
}

// HandleWebhook POST /api/v1/channel/:name/webhook
func (h *Hub) HandleWebhook(c *gin.Context) {
	if h == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "channel hub unavailable"})
		return
	}
	name := strings.TrimSpace(c.Param("name"))
	a, ok := h.adapter(name)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown or disabled channel"})
		return
	}
	if h.agent == nil || !h.agent.AgentConfigured() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Agent 未配置 AGENT_URL"})
		return
	}

	raw, err := c.GetRawData()
	if err != nil || len(raw) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty body"})
		return
	}

	res, err := a.HandleWebhook(c.Request.Context(), WebhookInput{
		RawBody: raw,
		Header:  c.Request.Header,
	})
	if err != nil {
		zap.L().Warn("channel webhook error", zap.String("channel", name), zap.Error(err))
		if res.HTTPStatus > 0 {
			c.JSON(res.HTTPStatus, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if res.HTTPStatus <= 0 {
		res.HTTPStatus = http.StatusOK
	}
	if res.Async != nil {
		in := res.Async
		if in.Channel == "" {
			in.Channel = name
		}
		if h.store != nil && in.EventID != "" {
			first, derr := h.store.MarkEventProcessed(c.Request.Context(), in.Channel, in.EventID)
			if derr != nil {
				zap.L().Warn("channel dedup failed", zap.Error(derr))
			} else if !first {
				c.JSON(res.HTTPStatus, res.Body)
				return
			}
		}
		if !ShouldProcess(in) {
			if hint := UnsupportedHint(in); hint != "" {
				go h.sendAsync(a, outboundFromInbound(in, hint))
			}
			if res.Body != nil {
				c.JSON(res.HTTPStatus, res.Body)
			} else {
				c.Status(res.HTTPStatus)
			}
			return
		}
		go h.processInbound(a, in)
	}
	if res.Body != nil {
		c.JSON(res.HTTPStatus, res.Body)
		return
	}
	c.Status(res.HTTPStatus)
}

func (h *Hub) processInbound(a Adapter, in *Inbound) {
	if in == nil || a == nil || h.agent == nil || h.store == nil || h.chat == nil {
		return
	}
	if !h.agent.TryAcquireAgentSlot() {
		h.sendAsync(a, outboundFromInbound(in, "服务繁忙，请稍后再试。"))
		return
	}
	defer h.agent.ReleaseAgentSlot()

	ctx, cancel := context.WithTimeout(context.Background(), h.agent.HTTPClientTimeout())
	defer cancel()

	uid, err := h.store.GetOrCreateSyntheticUserID(ctx, in.Channel, in.TenantID, in.ExternalUser)
	if err != nil {
		zap.L().Warn("channel user map failed", zap.String("channel", in.Channel), zap.Error(err))
		h.sendAsync(a, outboundFromInbound(in, "会话初始化失败，请稍后再试。"))
		return
	}
	sid, err := h.store.GetOrCreateChatSessionID(ctx, h.chat, in.Channel, in.TenantID, uid, in.ChatKey)
	if err != nil {
		zap.L().Warn("channel session failed", zap.String("channel", in.Channel), zap.Error(err))
		h.sendAsync(a, outboundFromInbound(in, "会话初始化失败，请稍后再试。"))
		return
	}

	reqID := fmt.Sprintf("%s-%s", in.Channel, uuid.NewString())
	zap.L().Info("channel chat start",
		zap.String("channel", in.Channel),
		zap.String("external_user", in.ExternalUser),
		zap.Uint64("user_id", uid),
		zap.String("chat_session_id", sid),
		zap.String("request_id", reqID),
	)

	runRes, err := h.agent.RunAgentChat(ctx, handler.AgentChatRunInput{
		UserID:        uid,
		Message:       in.Text,
		Token:         "",
		ChatSessionID: sid,
		RequestID:     reqID,
	}, nil)
	if err != nil {
		zap.L().Warn("channel agent failed", zap.String("channel", in.Channel), zap.Error(err))
		h.sendAsync(a, outboundFromInbound(in, "助手暂时不可用，请稍后再试。"))
		return
	}
	reply := strings.TrimSpace(runRes.Assistant)
	if reply == "" || runRes.StreamErr {
		reply = "抱歉，这次没有生成有效回复，请换个说法再试一次。"
	}
	h.sendAsync(a, outboundFromInbound(in, reply))
}

func outboundFromInbound(in *Inbound, text string) Outbound {
	meta := make(map[string]string, len(in.Meta))
	for k, v := range in.Meta {
		meta[k] = v
	}
	return Outbound{
		Channel:  in.Channel,
		TenantID: in.TenantID,
		Text:     text,
		Meta:     meta,
	}
}

func (h *Hub) sendAsync(a Adapter, out Outbound) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := a.Send(ctx, out); err != nil {
			zap.L().Warn("channel send failed",
				zap.String("channel", out.Channel),
				zap.Error(err),
			)
		}
	}()
}

// MountRoutes 注册各通道 webhook；name 与 Adapter.Name() 一致。
func (h *Hub) MountRoutes(g *gin.RouterGroup) {
	if h == nil {
		return
	}
	g.POST("/channel/:name/webhook", h.HandleWebhook)
	// 兼容早期飞书路径
	if _, ok := h.adapter(NameFeishu); ok {
		g.POST("/channel/feishu/event", func(c *gin.Context) {
			c.Params = gin.Params{{Key: "name", Value: NameFeishu}}
			h.HandleWebhook(c)
		})
	}
}
