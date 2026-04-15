package models

import "time"

// DirectMessage 私信消息。
// 联合主键：from_user_id + to_user_id + sent_at
// 每条消息一周后过期（expire_at）。
type DirectMessage struct {
	FromUserID uint64    `gorm:"primaryKey;column:from_user_id" json:"fromUserId,string"`
	ToUserID   uint64    `gorm:"primaryKey;column:to_user_id" json:"toUserId,string"`
	SentAt     time.Time `gorm:"primaryKey;column:sent_at" json:"sentAt"`
	Content    string    `gorm:"column:content;type:text;not null" json:"content"`
	ExpireAt   time.Time `gorm:"column:expire_at;index" json:"expireAt"`
}

type ParamSendDM struct {
	ToUserID uint64 `json:"toUserId,string" binding:"required"`
	Content  string `json:"content" binding:"required"`
}

type DMPeer struct {
	UserID   uint64 `json:"user_id,string"`
	UserName string `json:"user_name"`
}

type DMMessageDTO struct {
	FromUserID uint64 `json:"fromUserId,string"`
	ToUserID   uint64 `json:"toUserId,string"`
	SentAt     string `json:"sentAt"`
	Content    string `json:"content"`
}
