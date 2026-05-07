package models

import "time"

type Follow struct {
	ID uint64 `gorm:"primaryKey"`

	// 发起关注的人
	FollowerID uint64 `gorm:"uniqueIndex:idx_follow_unique"`
	// 被关注的人
	FollowingID uint64 `gorm:"uniqueIndex:idx_follow_unique;index:idx_following"`
	// 关注时间
	CreatedAt time.Time
}

type ParamFollow struct {
	FollowerID  uint64 `json:"followerId,string" binding:"required"`
	FollowingID uint64 `json:"followingId,string" binding:"required"`
}

// TopicFollow 用户对话题的关注（只与 topic_id 绑定；游戏与话题的对应关系见 GameTopicMap）。
type TopicFollow struct {
	UserID    uint64    `gorm:"primaryKey;column:user_id" json:"userId,string"`
	TopicID   uint      `gorm:"primaryKey;column:topic_id" json:"topicId"`
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
}

// TopicFollowListItem 关注话题列表（含话题名称，供前端 / MCP）。
type TopicFollowListItem struct {
	UserID    uint64    `json:"userId,string"`
	TopicID   uint      `json:"topicId"`
	TopicName string    `json:"topicName"`
	CreatedAt time.Time `json:"createdAt"`
}
