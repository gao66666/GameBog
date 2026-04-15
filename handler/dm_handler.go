package handler

import (
	"encoding/json"
	"strconv"

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

	// 在线则实时推送给对方（消息已落库；推送失败不影响发送成功）
	h.pushDMToUser(p.ToUserID, uid, m)

	tool.ResponseSuccess(c, gin.H{"sent_at": m.SentAt.Format("2006-01-02 15:04:05")}, "ok")
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
	}
}
