package models

import (
	"time"
)

type Article struct {
	ID         uint64 `gorm:"primaryKey;column:id" json:"id"`
	AuthorID   uint64 `gorm:"column:author_id;default:0" json:"authorId"`
	Title      string `gorm:"column:title;size:200;not null" json:"title"`
	Summary    string `gorm:"column:summary;size:500" json:"summary"`
	Content    string `gorm:"column:content;type:longtext" json:"content"`
	ViewCount  uint64 `gorm:"column:view_count;default:0" json:"viewCount"`
	LikeCount  uint64 `gorm:"column:like_count;default:0" json:"likeCount"`
	CategoryID uint   `gorm:"column:category_id" json:"categoryId"`

	// 关联部分
	Tags      []Tag     `gorm:"many2many:article_tags;" json:"tags"`
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

type Tag struct {
	ID   uint   `gorm:"primaryKey" json:"id"`
	Name string `gorm:"column:name;size:50;unique;not null" json:"name"`
}

type ParamPostArticle struct {
	Title      string `json:"title" binding:"required"`
	Content    string `json:"content" binding:"required"`
	CategoryID uint   `json:"section_id"` // 板块ID，可以先传0
}
