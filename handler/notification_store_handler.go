package handler

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/mq"
	"go.uber.org/zap"
)

type NotificationStoreHandler struct {
	repo          *database.NotificationRepository
	mu            sync.Mutex
	buffer        []*models.Notification
	batchSize     int
	flushInterval time.Duration
}

func NewNotificationStoreHandler(repo *database.NotificationRepository) *NotificationStoreHandler {
	return &NotificationStoreHandler{
		repo:          repo,
		buffer:        make([]*models.Notification, 0, 512),
		batchSize:     500,
		flushInterval: 10 * time.Second,
	}
}

func (h *NotificationStoreHandler) StartFlushTicks() {
	       ticker := time.NewTicker(h.flushInterval)
	       go func() {
		       for range ticker.C {
			       if err := h.flush(); err != nil {
				       zap.L().Warn("通知批量落库失败", zap.Error(err))
			       }
		       }
	       }()
}

func (h *NotificationStoreHandler) ProcessNotificationStoreMessage(ctx context.Context, payload []byte) error {
	var p mq.NotificationPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		zap.L().Warn("通知落库消息解析失败，直接丢弃", zap.Error(err))
		return nil
	}

	now := time.Now()
	created := time.Unix(p.CreatedAt, 0)
	if p.CreatedAt == 0 {
		created = now
	}
	notification := &models.Notification{
		EventID:    p.EventID,
		UserID:     p.UserID,
		SenderID:   p.SenderID,
		SenderName: p.SenderName,
		Content:    p.Content,
		Type:       p.Type,
		IsRead:     p.IsRead,
		CreatedAt:  created,
		UpdatedAt:  now,
	}
	if p.IsRead {
		t := now
		notification.ReadAt = &t
	}

	h.mu.Lock()
	h.buffer = append(h.buffer, notification)
	shouldFlush := len(h.buffer) >= h.batchSize
	h.mu.Unlock()

	if shouldFlush {
		return h.flush()
	}
	return nil
}

func (h *NotificationStoreHandler) flush() error {
	h.mu.Lock()
	batch := make([]*models.Notification, len(h.buffer))
	copy(batch, h.buffer)
	h.buffer = h.buffer[:0]
	h.mu.Unlock()

	if len(batch) == 0 {
		return nil
	}

	if err := h.repo.BatchCreateNotifications(batch); err != nil {
		h.mu.Lock()
		h.buffer = append(batch, h.buffer...)
		h.mu.Unlock()
		return err
	}

	return nil
}

func (h *NotificationStoreHandler) MarkAllRead(userID uint64) error {
	return h.repo.MarkReadByUserID(userID)
}

func (h *NotificationStoreHandler) LoadUnread(userID uint64) ([]*models.Notification, error) {
	return h.repo.GetUnreadByUserID(userID)
}
