package models

import "time"

// GameLicenseCode 游戏库库存（一行一码，购买时于事务内占用）。
type GameLicenseCode struct {
	ID        uint64     `gorm:"primaryKey;column:id" json:"id,string"`
	GameID    uint64     `gorm:"column:game_id;not null;index:idx_game_code_status,priority:1" json:"gameId,string"`
	Code      string     `gorm:"column:code;size:64;not null;uniqueIndex" json:"code"`
	Status    string     `gorm:"column:status;size:16;not null;default:available;index:idx_game_code_status,priority:2" json:"status"`
	OrderID   uint64     `gorm:"column:order_id;index" json:"orderId,string"`
	BuyerID   uint64     `gorm:"column:buyer_id;index" json:"buyerId,string"`
	SoldAt    *time.Time `gorm:"column:sold_at" json:"soldAt,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at" json:"createdAt"`
}

func (GameLicenseCode) TableName() string { return "game_license_codes" }

// GameOrder 游戏库购买订单。
type GameOrder struct {
	OrderID    uint64    `gorm:"primaryKey;column:order_id" json:"orderId,string"`
	UserID     uint64    `gorm:"column:user_id;not null;index" json:"userId,string"`
	GameID     uint64    `gorm:"column:game_id;not null;index" json:"gameId,string"`
	GameName   string    `gorm:"column:game_name;size:200;not null" json:"gameName"`
	PriceCents int64     `gorm:"column:price_cents;not null" json:"priceCents"`
	Code       string    `gorm:"column:code;size:64;not null" json:"code"`
	CreatedAt  time.Time `gorm:"column:created_at" json:"createdAt"`
}

func (GameOrder) TableName() string { return "game_orders" }

// AccountTransaction 账户余额流水（游戏购买等扣款）。
type AccountTransaction struct {
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

func (AccountTransaction) TableName() string { return "account_transactions" }

const (
	GameCodeAvailable = "available"
	GameCodeSold      = "sold"
)
