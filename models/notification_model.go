package models

import "time"

// Notification 通知实体，MySQL 持久化
// EventID 作为业务唯一标识，避免 Kafka 重投时重复插入。
type Notification struct {
	EventID    uint64     `gorm:"primaryKey;autoIncrement:false" json:"eventId"`
	UserID     uint64     `gorm:"index:idx_user_read_time,priority:1;not null" json:"userId"`
	SenderID   uint64     `gorm:"index;default:0" json:"senderId"`
	SenderName string     `gorm:"size:100;not null;default:''" json:"senderName"`
	Content    string     `gorm:"type:text;not null" json:"content"`
	Type       string     `gorm:"size:32;not null;index" json:"type"`
	IsRead     bool       `gorm:"index:idx_user_read_time,priority:2;default:false" json:"isRead"`
	ReadAt     *time.Time `gorm:"column:read_at" json:"readAt,omitempty"`
	CreatedAt   time.Time  `gorm:"index:idx_user_read_time,priority:3" json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}
