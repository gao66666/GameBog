package models

import (
	"time"
)

type Article struct {
	ID           uint64 `gorm:"primaryKey;column:id" json:"id,string"`
	AuthorID     uint64 `gorm:"column:author_id;default:0" json:"authorId,string"`
	Title        string `gorm:"column:title;size:200;not null" json:"title"`
	Summary      string `gorm:"column:summary;size:500" json:"summary"`
	CoverURL     string `gorm:"column:cover_url;size:512;default:''" json:"coverUrl"`
	Content      string `gorm:"column:content;type:longtext" json:"content"`
	ViewCount    uint64 `gorm:"column:view_count;default:0" json:"viewCount,string"`
	LikeCount    uint64 `gorm:"column:like_count;default:0" json:"likeCount,string"`
	CommentCount int64  `gorm:"-" json:"commentCount"`
	CategoryID   uint   `gorm:"column:category_id" json:"categoryId"`

	// 关联部分
	Tags      []Tag     `gorm:"many2many:article_tags;" json:"tags"`
	GameIDs   []uint64  `gorm:"-" json:"gameIds,omitempty"`
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// ArticleLike 文章点赞关系表（唯一行为记录）。
// 通过 (user_id, article_id) 唯一约束保证幂等：
// - 重复点赞不会重复计数
// - 取消点赞只在记录存在时生效
type ArticleLike struct {
	UserID    uint64    `gorm:"primaryKey;column:user_id" json:"userId"`
	ArticleID uint64    `gorm:"primaryKey;column:article_id" json:"articleId"`
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
}

// ArticleCollection 文章收藏。
type ArticleCollection struct {
	UserID    uint64    `gorm:"primaryKey;column:user_id" json:"userId,string"`
	ArticleID uint64    `gorm:"primaryKey;column:article_id" json:"articleId,string"`
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
}

// ArticleCollectionItem 我的收藏列表 API 项（含标题、摘要、标签）。
type ArticleCollectionItem struct {
	ArticleID   uint64    `json:"articleId,string"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	CoverURL    string    `json:"coverUrl"`
	Tags        []Tag     `json:"tags"`
	CollectedAt time.Time `json:"collectedAt"`
}

type Tag struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"column:name;size:50;unique;not null" json:"name"`
}

type ParamPostArticle struct {
	Title      string   `json:"title" binding:"required"`
	Summary    string   `json:"summary" binding:"required"`
	Content    string   `json:"content" binding:"required"`
	CoverURL   string   `json:"cover_url"`
	Tags       []string `json:"tags"`
	GameIDs    []uint64 `json:"game_ids"`
	CategoryID uint     `json:"section_id"` // 板块ID，可以先传0
}

type ArticleGame struct {
	ArticleID uint64    `gorm:"primaryKey;column:article_id;index"`
	GameID    uint64    `gorm:"primaryKey;column:game_id;index"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

type ArticleTopic struct {
	ArticleID uint64    `gorm:"primaryKey;column:article_id;index"`
	TopicID   uint      `gorm:"primaryKey;column:topic_id;index"`
	CreatedAt time.Time `gorm:"column:created_at"`
}
