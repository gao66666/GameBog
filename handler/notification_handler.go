package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/jwt_module"
	"github.com/gao66666/GoBlog/mq"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"github.com/olahol/melody"
	"go.uber.org/zap"
)

type NotificationHandler struct {
	Meld              *melody.Melody
	Conns             sync.Map // 存储 key: uint64(UserID), value: *melody.Session
	notificationRepo  *database.NotificationRepository
	redisNotification *database.RedisNotificationRepository
}

// PushNotification 用于“无 Kafka 时”的降级直推：只要用户在线（有活跃 WS 连接）就直接写入。
// 返回 true 表示已成功推送。
func (nh *NotificationHandler) PushNotification(userID uint64, payload []byte) bool {
	if nh == nil || userID == 0 || len(payload) == 0 {
		return false
	}
	val, ok := nh.Conns.Load(userID)
	if !ok {
		return false
	}
	s, ok := val.(*melody.Session)
	if !ok || s == nil || s.IsClosed() {
		nh.Conns.Delete(userID)
		return false
	}
	if err := s.Write(payload); err != nil {
		nh.Conns.Delete(userID)
		return false
	}
	return true
}

func NewNotificationHandler(notificationRepo *database.NotificationRepository, redisNotification *database.RedisNotificationRepository) *NotificationHandler {
	m := melody.New()
	nh := &NotificationHandler{
		Meld:              m,
		notificationRepo:  notificationRepo,
		redisNotification: redisNotification,
	}

	// 握手成功后，自动触发这个钩子
	m.HandleConnect(func(s *melody.Session) {
		// 从我们在 HandleWS 存入的 Keys 里取值
		if uid, ok := s.Get("userID"); ok {
			// 签到：UID -> Session
			userID := uid.(uint64)
			nh.Conns.Store(userID, s)
			// 只在打开 /me 时才同步离线通知（避免在其它页面登录就把通知拉走/标已读）
			if v, ok := s.Get("syncUnread"); ok {
				if b, ok := v.(bool); ok && b {
					go nh.syncUnreadNotifications(userID, s)
				}
			}
		}
	})

	m.HandleDisconnect(func(s *melody.Session) {
		if uid, ok := s.Get("userID"); ok {
			// 离线：删掉记录
			nh.Conns.Delete(uid.(uint64))
		}
	})

	return nh
}

// internal/handler/ws_handler.go

func (nh *NotificationHandler) HandleWS(c *gin.Context) {
	// 1. 提取 Token
	token := c.Query("token")
	if token == "" {
		tool.ResponseError(c, jwt_module.ErrInvalidToken)
		return
	}

	// 2. 校验 Token
	claims, err := jwt_module.ParseToken(token)
	if err != nil {
		tool.ResponseError(c, jwt_module.ErrInvalidToken)
		return
	}

	// 3. 升级连接
	// 将解析出的 UID 存入 Melody Session 的 Keys 中
	syncUnread := c.Query("sync") == "1"
	err = nh.Meld.HandleRequestWithKeys(c.Writer, c.Request, map[string]interface{}{
		"userID":     claims.UserID,
		"syncUnread": syncUnread,
	})

	if err != nil {
		zap.L().Error("WebSocket 升级失败", zap.Error(err))
	}
}

// GetUserOnlineStatus 公开查询用户在线状态（WebSocket 是否存在活跃连接）。
func (nh *NotificationHandler) GetUserOnlineStatus(c *gin.Context) {
	idStr := c.Param("id")
	uid, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || uid == 0 {
		tool.ResponseError(c, ErrCodeInvalidParam)
		return
	}
	_, ok := nh.Conns.Load(uid)
	tool.ResponseSuccess(c, gin.H{"online": ok}, "ok")
}

func (nh *NotificationHandler) syncUnreadNotifications(userID uint64, session *melody.Session) {
	if nh.redisNotification == nil || nh.notificationRepo == nil {
		return
	}

	hasUnread, err := nh.redisNotification.HasUnread(userID)
	if err != nil || !hasUnread {
		return
	}

	notifications, err := nh.notificationRepo.GetUnreadByUserID(userID)
	if err != nil || len(notifications) == 0 {
		return
	}

	for _, item := range notifications {
		if session.IsClosed() {
			return
		}
		body, err := json.Marshal(item)
		if err != nil {
			zap.L().Warn("序列化离线通知失败", zap.Error(err))
			return
		}
		if err := session.Write(body); err != nil {
			zap.L().Warn("同步离线通知失败", zap.Uint64("uid", userID), zap.Error(err))
			return
		}
	}

	if err := nh.redisNotification.ClearHasUnread(userID); err != nil {
		zap.L().Warn("清理未读红点失败", zap.Uint64("uid", userID), zap.Error(err))
	}

	go func() {
		if err := nh.notificationRepo.MarkReadByUserID(userID); err != nil {
			zap.L().Warn("离线通知标记已读失败", zap.Uint64("uid", userID), zap.Error(err))
		}
	}()
}

func (nh *NotificationHandler) ProcessNotificationMessage(ctx context.Context, payload []byte) error {
	var p mq.NotificationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		zap.L().Error("解析通知 Payload 失败", zap.Error(err))
		return nil
	}

	// 2. 检查用户是否在线
	val, ok := nh.Conns.Load(p.UserID)
	if !ok {
		nh.persistOfflineNotification(p)
		zap.L().Debug("用户不在线，跳过推送", zap.Uint64("uid", p.UserID))
		return nil
	}

	// 3. 执行推送
	session, ok := val.(*melody.Session)
	if !ok || session.IsClosed() {
		// 如果 Session 已经失效但还没来得及从 Map 里删掉
		nh.Conns.Delete(p.UserID)
		return nil
	}

	msg, _ := json.Marshal(p)
	err := session.Write(msg)
	if err != nil {
		nh.persistOfflineNotification(p)
		return fmt.Errorf("WebSocket写入失败: %w", err)
	}

	zap.L().Info("实时推送成功", zap.Uint64("to_uid", p.UserID), zap.String("type", p.Type))
	return nil
}

func (nh *NotificationHandler) persistOfflineNotification(p mq.NotificationPayload) {
	if nh.redisNotification != nil {
		_ = nh.redisNotification.SetHasUnread(p.UserID)
	}
	if err := mq.PublishNotificationStore(p); err != nil {
		zap.L().Error("通知离线落库消息发送失败", zap.Error(err))
	}
}
