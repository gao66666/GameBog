package database

import (
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInitNotification      = tool.NewBizError(404, 40011, "通知数据库初始化出错")
	ErrNotificationBatchSave = tool.NewBizError(404, 40011, "批量保存通知失败")
)

type NotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) InitTable() error {
	if err := r.db.AutoMigrate(&models.Notification{}); err != nil {
		return ErrInitNotification
	}
	zap.L().Info("通知表初始化成功")
	return nil
}

func (r *NotificationRepository) BatchCreateNotifications(notifications []*models.Notification) error {
	if len(notifications) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "event_id"}},
			DoNothing: true,
		}).CreateInBatches(notifications, 500).Error
	})
}

func (r *NotificationRepository) GetUnreadByUserID(userID uint64) ([]*models.Notification, error) {
	var notifications []*models.Notification
	if err := r.db.Where("user_id = ? AND is_read = ?", userID, false).
		Order("created_at ASC").
		Find(&notifications).Error; err != nil {
		return nil, err
	}
	return notifications, nil
}

func (r *NotificationRepository) MarkReadByUserID(userID uint64) error {
	now := time.Now()
	return r.db.Model(&models.Notification{}).
		Where("user_id = ? AND is_read = ?", userID, false).
		Updates(map[string]interface{}{
			"is_read": true,
			"read_at": &now,
		}).Error
}
