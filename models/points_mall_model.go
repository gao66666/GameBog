package models

import "time"

// PointsMallProduct 积分商城商品（游戏激活码类）。
type PointsMallProduct struct {
	ID          uint64    `gorm:"primaryKey;column:id" json:"id,string"`
	Name        string    `gorm:"column:name;size:128;not null" json:"name"`
	Subtitle    string    `gorm:"column:subtitle;size:128" json:"subtitle"`
	Description string    `gorm:"column:description;type:text" json:"description"`
	PricePoints int64     `gorm:"column:price_points;not null" json:"pricePoints"`
	CoverURL    string    `gorm:"column:cover_url;size:512" json:"coverUrl"`
	SortOrder   int       `gorm:"column:sort_order;not null;default:0" json:"sortOrder"`
	Status      string    `gorm:"column:status;size:16;not null;default:on_sale" json:"status"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt   time.Time `gorm:"column:updated_at" json:"updatedAt"`
}

func (PointsMallProduct) TableName() string { return "points_mall_products" }

// PointsMallCode 激活码库存（一行一码）。
type PointsMallCode struct {
	ID        uint64     `gorm:"primaryKey;column:id" json:"id,string"`
	ProductID uint64     `gorm:"column:product_id;not null;index:idx_mall_code_product_status,priority:1" json:"productId,string"`
	Code      string     `gorm:"column:code;size:64;not null;uniqueIndex" json:"code"`
	Status    string     `gorm:"column:status;size:16;not null;default:available;index:idx_mall_code_product_status,priority:2" json:"status"`
	OrderID   uint64     `gorm:"column:order_id;index" json:"orderId,string"`
	BuyerID   uint64     `gorm:"column:buyer_id;index" json:"buyerId,string"`
	SoldAt    *time.Time `gorm:"column:sold_at" json:"soldAt,omitempty"`
	CreatedAt time.Time  `gorm:"column:created_at" json:"createdAt"`
}

func (PointsMallCode) TableName() string { return "points_mall_codes" }

const (
	MallCodeAvailable = "available"
	MallCodeSold      = "sold"
	MallProductOnSale = "on_sale"
)

// PointsMallOrder 兑换订单。
type PointsMallOrder struct {
	OrderID     uint64    `gorm:"primaryKey;column:order_id" json:"orderId,string"`
	UserID      uint64    `gorm:"column:user_id;not null;index" json:"userId,string"`
	ProductID   uint64    `gorm:"column:product_id;not null;index" json:"productId,string"`
	ProductName string    `gorm:"column:product_name;size:128;not null" json:"productName"`
	PointsSpent int64     `gorm:"column:points_spent;not null" json:"pointsSpent"`
	Code        string    `gorm:"column:code;size:64;not null" json:"code"`
	CreatedAt   time.Time `gorm:"column:created_at" json:"createdAt"`
}

func (PointsMallOrder) TableName() string { return "points_mall_orders" }

// PointsMallProductVO 列表展示（含 Redis 实时库存）。
type PointsMallProductVO struct {
	ID          uint64 `json:"id,string"`
	Name        string `json:"name"`
	Subtitle    string `json:"subtitle"`
	Description string `json:"description"`
	PricePoints int64  `json:"pricePoints"`
	CoverURL    string `json:"coverUrl"`
	Stock       int64  `json:"stock"`
	Status      string `json:"status"`
}
