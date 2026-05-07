package models

import (
	"time"

	"gorm.io/datatypes"
)

// Game 游戏基础信息
type Game struct {
	ID           uint64         `gorm:"primaryKey;column:id" json:"id,string"`
	Name         string         `gorm:"column:name;size:200;not null;index" json:"name"`
	Description  string         `gorm:"column:description;type:text" json:"description"`
	ReleaseAt    time.Time      `gorm:"column:release_date" json:"releaseAt"`
	Publisher    string         `gorm:"column:publisher;size:200" json:"publisher"`
	Developer    string         `gorm:"column:developer;size:200" json:"developer"`
	CoverURL     string         `gorm:"column:cover_url;size:512" json:"coverUrl"`
	PriceCents   int64          `gorm:"column:price_cents;not null;default:-1" json:"priceCents"` // -1 未设置；0 免费；>0 为分（¥×100）
	Tags         datatypes.JSON `gorm:"column:tags;type:json" json:"tags"`                        // JSON 字符串数组
	Achievements datatypes.JSON `gorm:"column:achievements;type:json" json:"achievements"`
	CreatedAt    time.Time      `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt    time.Time      `gorm:"column:updated_at" json:"updatedAt"`
}

// GameReview 游戏点评
type GameReview struct {
	ID         uint64    `gorm:"primaryKey;column:id" json:"id,string"`
	GameID     uint64    `gorm:"column:game_id;not null;index:idx_game_user,priority:1;index" json:"gameId,string"`
	UserID     uint64    `gorm:"column:user_id;not null;index:idx_game_user,priority:2;index" json:"userId,string"`
	ReviewedAt time.Time `gorm:"column:reviewed_at;not null;index" json:"reviewedAt"`
	Rating     uint8     `gorm:"column:rating;type:tinyint unsigned;not null;default:5" json:"rating"`
	Content    string    `gorm:"column:content;type:text;not null" json:"content"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt  time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

// GameReviewComment 对游戏点评的评论
type GameReviewComment struct {
	ID        uint64    `gorm:"primaryKey;column:id" json:"id,string"`
	ReviewID  uint64    `gorm:"column:review_id;not null;index:idx_review_parent,priority:1;index" json:"reviewId,string"`
	UserID    uint64    `gorm:"column:user_id;not null;index" json:"userId,string"`
	ParentID  uint64    `gorm:"column:parent_id;not null;default:0;index:idx_review_parent,priority:2" json:"parentId,string"`
	Content   string    `gorm:"column:content;type:text;not null" json:"content"`
	CreatedAt time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

type ParamCreateGame struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	ReleaseAt   string   `json:"releaseAt"`
	Publisher   string   `json:"publisher"`
	Developer   string   `json:"developer"`
	CoverURL    string   `json:"coverUrl"`
	PriceCents  *int64   `json:"priceCents"` // nil 表示默认 -1
	Tags        []string `json:"tags"`
}

type ParamUpdateGame struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	ReleaseAt   string   `json:"releaseAt"`
	Publisher   string   `json:"publisher"`
	Developer   string   `json:"developer"`
	CoverURL    string   `json:"coverUrl"`
	PriceCents  *int64   `json:"priceCents"`
	Tags        []string `json:"tags"`
}

type ParamCreateGameReview struct {
	Rating  uint8  `json:"rating" binding:"required"`
	Content string `json:"content" binding:"required"`
}

type ParamUpdateGameReview struct {
	Rating  uint8  `json:"rating" binding:"required"`
	Content string `json:"content" binding:"required"`
}

type ParamCreateGameReviewComment struct {
	ParentID uint64 `json:"parentId,string"`
	Content  string `json:"content" binding:"required"`
}

type ParamUpdateGameReviewComment struct {
	Content string `json:"content" binding:"required"`
}

// UserGamePlay 用户游戏游玩记录
type UserGamePlay struct {
	UserID         uint64         `gorm:"primaryKey;column:user_id" json:"userId,string"`
	GameID         uint64         `gorm:"primaryKey;column:game_id" json:"gameId,string"`
	PlaytimeTotal  float64        `gorm:"column:playtime_total;default:0" json:"playtimeTotal"`
	Playtime2Weeks float64        `gorm:"column:playtime_2weeks;default:0" json:"playtime2Weeks"`
	AchievedIDs    datatypes.JSON `gorm:"column:achieved_ids;type:json" json:"achievedIds"`
	UpdatedAt      time.Time      `gorm:"column:updated_at" json:"updatedAt"`
	CreatedAt      time.Time      `gorm:"column:created_at" json:"createdAt"`

	// 关联（方便 Preload）
	Game Game `gorm:"foreignKey:GameID" json:"game"`
}

type ParamUpsertUserGamePlay struct {
	GameID         string  `json:"gameId" binding:"required"`
	PlaytimeTotal  float64 `json:"playtimeTotal"`
	Playtime2Weeks float64 `json:"playtime2Weeks"`
	AchievedIDs    []uint  `json:"achievedIds"`
}

// UserWallet 积分账户
type UserWallet struct {
	UserID        uint64    `gorm:"primaryKey;column:user_id" json:"userId,string"`
	Balance       int64     `gorm:"column:balance;not null;default:0" json:"balance"`
	FrozenBalance int64     `gorm:"column:frozen_balance;not null;default:0" json:"frozenBalance"`
	Version       int       `gorm:"column:version;not null;default:0" json:"version"`
	UpdatedAt     time.Time `gorm:"column:updated_at" json:"updatedAt"`
	CreatedAt     time.Time `gorm:"column:created_at" json:"createdAt"`
}

// PointsTransaction 积分流水（不可变记录）
type PointsTransaction struct {
	TxnID        uint64    `gorm:"primaryKey;column:txn_id" json:"txnId,string"`
	UserID       uint64    `gorm:"column:user_id;not null;index" json:"userId,string"`
	Amount       int64     `gorm:"column:amount;not null" json:"amount"`
	Type         string    `gorm:"column:type;size:32;not null" json:"type"`
	RefType      string    `gorm:"column:ref_type;size:32;not null" json:"refType"`
	RefID        uint64    `gorm:"column:ref_id" json:"refId,string"`
	BalanceAfter int64     `gorm:"column:balance_after;not null" json:"balanceAfter"`
	Description  string    `gorm:"column:description;size:255" json:"description"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"createdAt"`
}

// PointsOutbox 积分事件发件箱（Transactional Outbox）
// 与业务操作在同一事务写入，保证业务成功则积分事件必达。
type PointsOutbox struct {
	ID          uint64    `gorm:"primaryKey;column:id" json:"id,string"`
	UserID      uint64    `gorm:"column:user_id;not null;index" json:"userId,string"`
	Amount      int64     `gorm:"column:amount;not null" json:"amount"`
	RefType     string    `gorm:"column:ref_type;size:32;not null" json:"refType"`
	RefID       uint64    `gorm:"column:ref_id" json:"refId,string"`
	Description string    `gorm:"column:description;size:255" json:"description"`
	Status      int       `gorm:"column:status;not null;default:0" json:"status"`          // 0=pending, 1=sent, 2=failed
	RetryCount  int       `gorm:"column:retry_count;not null;default:0" json:"retryCount"` // 重试次数
	CreatedAt   time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

const (
	PointsOutboxStatusPending = 0
	PointsOutboxStatusSent    = 1
	PointsOutboxStatusFailed  = 2
)
