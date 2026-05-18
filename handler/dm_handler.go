package handler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"github.com/olahol/melody"
	"go.uber.org/zap"
)

type DMHandler struct {
	se    *service.DMService
	notif *NotificationHandler
}

func NewDMHandler(se *service.DMService, notif *NotificationHandler) *DMHandler {
	return &DMHandler{se: se, notif: notif}
}

func (h *DMHandler) ListPeers(c *gin.Context) {
	uid := c.GetUint64("userID")
	peers, err := h.se.ListPeers(uid)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"peers": peers}, "ok")
}

func (h *DMHandler) ListMessages(c *gin.Context) {
	uid := c.GetUint64("userID")
	peerStr := c.Query("peer_id")
	peerID, _ := strconv.ParseUint(peerStr, 10, 64)
	if peerID == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	msgs, err := h.se.ListMessages(uid, peerID)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"messages": msgs}, "ok")
}

func (h *DMHandler) SendMessage(c *gin.Context) {
	uid := c.GetUint64("userID")
	var p models.ParamSendDM
	if err := c.ShouldBindJSON(&p); err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	if p.ToUserID == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	m, err := h.se.SendMessage(uid, p.ToUserID, p.Content)
	if err != nil {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	// 送达逻辑（仿 QQ）：
	// - 对方在线：直接 WS 推送这条消息
	// - 对方离线：Redis 未读数 +1（对方登录后看到 dm_unread 事件再拉取）
	if h.isUserOnline(p.ToUserID) {
		h.pushDMToUser(p.ToUserID, uid, m)
	} else {
		h.se.IncrUnread(p.ToUserID)
	}

	// 「新私信」提醒：先本机 WebSocket，离线则异步落库（与评论/关注通知同源）
	senderName := fmt.Sprintf("UID:%d", uid)
	if n := h.se.ResolveUserName(uid); n != "" {
		senderName = n
	}
	preview := strings.TrimSpace(m.Content)
	r := []rune(preview)
	if len(r) > 15 {
		preview = string(r[:15]) + "..."
	}
	content := fmt.Sprintf("%s 给你发了私信：%s", senderName, preview)
	if h.notif != nil {
		h.notif.NotifyPushOrStore(p.ToUserID, uid, senderName, content, "dm")
	}

	tool.ResponseSuccess(c, gin.H{"sent_at": m.SentAt.Format("2006-01-02 15:04:05")}, "ok")
}

func (h *DMHandler) GetUnreadCount(c *gin.Context) {
	uid := c.GetUint64("userID")
	count, err := h.se.GetUnreadCount(uid)
	if err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"count": count}, "ok")
}

func (h *DMHandler) ClearUnread(c *gin.Context) {
	uid := c.GetUint64("userID")
	h.se.ClearUnread(uid)
	tool.ResponseSuccess(c, gin.H{"ok": true}, "ok")
}

func (h *DMHandler) DeleteConversation(c *gin.Context) {
	uid := c.GetUint64("userID")
	var body struct {
		PeerID uint64 `json:"peerId"`
	}
	_ = c.ShouldBindJSON(&body)
	peerID := body.PeerID
	if peerID == 0 {
		peerStr := c.Query("peer_id")
		pid, _ := strconv.ParseUint(peerStr, 10, 64)
		peerID = pid
	}
	if peerID == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}

	if err := h.se.DeleteConversation(uid, peerID); err != nil {
		tool.ResponseError(c, CodeServerBusy)
		return
	}
	tool.ResponseSuccess(c, gin.H{"ok": true}, "ok")
}

func (h *DMHandler) isUserOnline(userID uint64) bool {
	if h == nil || h.notif == nil || userID == 0 {
		return false
	}
	val, ok := h.notif.Conns.Load(userID)
	if !ok {
		return false
	}
	s, ok := val.(*melody.Session)
	return ok && s != nil && !s.IsClosed()
}

func (h *DMHandler) pushDMToUser(toUserID uint64, fromUserID uint64, m *models.DirectMessage) {
	if h == nil || h.notif == nil || toUserID == 0 || fromUserID == 0 || m == nil {
		return
	}

	val, ok := h.notif.Conns.Load(toUserID)
	if !ok {
		return
	}
	session, ok := val.(*melody.Session)
	if !ok || session.IsClosed() {
		h.notif.Conns.Delete(toUserID)
		return
	}

	payload := map[string]any{
		"type":       "dm",
		"fromUserId": strconv.FormatUint(fromUserID, 10),
		"toUserId":   strconv.FormatUint(toUserID, 10),
		"sentAt":     m.SentAt.Format("2006-01-02 15:04:05"),
		"content":    m.Content,
	}
	b, _ := json.Marshal(payload)
	if err := session.Write(b); err != nil {
		zap.L().Warn("私信WS推送失败", zap.Uint64("to_uid", toUserID), zap.Error(err))
		h.notif.Conns.Delete(toUserID)
		return
	}
	h.notif.touchWSIdleTimer(toUserID)
}
