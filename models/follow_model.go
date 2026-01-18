package models

import "time"

type Follow struct {
	ID uint64 `gorm:"primaryKey"`

	// 发起关注的人
	FollowerID uint64 `gorm:"uniqueIndex:idx_follow_unique"`
	// 被关注的人
	FollowingID uint64 `gorm:"uniqueIndex:idx_follow_unique;index:idx_following"`
	// 关注时间
	CreatedAt time.Time `json:"created_at"`
}

type ParamFollow struct {
	FollowerID  uint64 `json:"followerId,string" binding:"required"`
	FollowingID uint64 `json:"followingId,string" binding:"required"`
}
