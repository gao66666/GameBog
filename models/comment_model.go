package models

import (
	"time"
)

// 从前端传到后端的结构体参数
type ParamComment struct {
	// 加上 ,string，Gin 解析时会自动把前端的字符串 "123" 转成 uint64
	ArticleID uint64 `json:"articleId,string" binding:"required"`
	UserID    uint64 `json:"userId,string"    binding:"required"`
	ParentID  uint64 `json:"parentId,string"`
	Content   string `json:"content" binding:"required,max=500"` // 限制字数
}

// 数据库存储的评论结构体
type Comment struct {
	ArticleID uint64 `gorm:"primaryKey;autoIncrement:false;index:idx_art_root_like_id,priority:1"`
	ID        uint64 `gorm:"primaryKey;autoIncrement:false"` // 雪花ID
	UserID    uint64 `gorm:"not null"`

	// 树形字段
	RootID      uint64 `gorm:"index:idx_art_root_like_id,priority:2;index:idx_root_id"`
	ParentID    uint64 `gorm:"not null;default:0"`
	ReplyUserID uint64 `gorm:"not null;default:0"`

	Content   string    `gorm:"type:text;not null"`
	LikeCount uint32    `gorm:"index:idx_art_root_like_id,priority:3;default:0"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	User      User      `gorm:"foreignKey:UserID;references:ID"`
	ReplyUser User      `gorm:"foreignKey:ReplyUserID;references:ID"`
}

// 发给前端评论区的用户结构体
type CommentUser struct {
	UserID   uint64 `json:"userId,string"`
	UserName string `json:"userName"`
	Avatar   string `json:"avatar"` // 头像
}

// 转换给前端的结构体
type CommentVO struct {
	ID        uint64       `json:"id,string"`
	RootID    uint64       `json:"rootId,string"`
	User      CommentUser  `json:"user"` // 当前评论者
	ReplyUser *CommentUser `json:"replyUser,omitempty"`
	Content   string       `json:"content"`
	LikeCount uint32       `json:"likeCount"` // 统一小驼峰
	Children  []*CommentVO `json:"children,omitempty"`
}
