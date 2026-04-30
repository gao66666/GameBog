package models

import "time"

// Topic 话题：
// - 长期话题：IsTemporary=false，ExpiresAt=nil
// - 临时话题：IsTemporary=true，ExpiresAt=创建时间+48h
//
// 文章通过 Article.CategoryID 关联到 Topic.ID（历史字段名 section_id）。
type Topic struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Name        string     `gorm:"column:name;size:50;unique;not null" json:"name"`
	IsTemporary bool       `gorm:"column:is_temporary;default:false" json:"isTemporary"`
	ExpiresAt   *time.Time `gorm:"column:expires_at" json:"expiresAt,omitempty"`
	CreatedAt   time.Time  `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt   time.Time  `gorm:"column:updated_at" json:"updatedAt"`
}

// TopicDiscussion 话题讨论区（扁平帖子）。
type TopicDiscussion struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement:false;column:id" json:"id,string"`
	TopicID   uint      `gorm:"column:topic_id;index;not null" json:"topicId"`
	UserID    uint64    `gorm:"column:user_id;index;not null" json:"userId,string"`
	Content   string    `gorm:"column:content;size:500;not null" json:"content"`
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
}

// GameTopicMap 游戏与话题映射（一个游戏一个专属话题）。
type GameTopicMap struct {
	GameID    uint64    `gorm:"primaryKey;column:game_id" json:"gameId,string"`
	TopicID   uint      `gorm:"column:topic_id;not null;index" json:"topicId"`
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}
