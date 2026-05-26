package database

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrMallProductNotFound = tool.NewBizError(404, 40030, "商品不存在或已下架")
	ErrMallOutOfStock      = tool.NewBizError(400, 40031, "商品已售罄")
	ErrMallRedeemBusy      = tool.NewBizError(409, 40032, "兑换处理中，请稍后重试")
)

type PointsMallRepository struct {
	db *gorm.DB
}

func NewPointsMallRepository(db *gorm.DB) *PointsMallRepository {
	return &PointsMallRepository{db: db}
}

func (r *PointsMallRepository) InitTable() error {
	if err := r.db.AutoMigrate(
		&models.PointsMallProduct{},
		&models.PointsMallCode{},
		&models.PointsMallOrder{},
	); err != nil {
		return err
	}
	if err := r.seedCatalogIfEmpty(); err != nil {
		return err
	}
	zap.L().Info("积分商城表初始化成功")
	return nil
}

func (r *PointsMallRepository) seedCatalogIfEmpty() error {
	var n int64
	if err := r.db.Model(&models.PointsMallProduct{}).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	type seedProduct struct {
		id          uint64
		name        string
		subtitle    string
		description string
		price       int64
		cover       string
		sort        int
		codePrefix  string
		codeCount   int
	}

	seeds := []seedProduct{
		{
			id: 910001, name: "巫师 3：狂猎", subtitle: "The Witcher 3: Wild Hunt",
			description: "CD PROJEKT RED 开放世界 RPG 经典之作。兑换后获得 GOG/Steam 风格演示激活码（仅供本项目积分商城演示）。",
			price: 30, cover: "https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg", sort: 1,
			codePrefix: "W3WILD", codeCount: 30,
		},
		{
			id: 910002, name: "荒野大镖客：救赎 2", subtitle: "Red Dead Redemption 2",
			description: "Rockstar 西部史诗。兑换后获得平台演示激活码（仅供本项目积分商城演示）。",
			price: 40, cover: "https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg", sort: 2,
			codePrefix: "RDR2", codeCount: 25,
		},
		{
			id: 910003, name: "天国：拯救", subtitle: "Kingdom Come: Deliverance",
			description: "Warhorse 中世纪写实 RPG。兑换后获得平台演示激活码（仅供本项目积分商城演示）。",
			price: 25, cover: "https://pic4.zhimg.com/v2-cad31f1efa6d4940651ebec9063fd5cb_r.jpg", sort: 3,
			codePrefix: "KCD", codeCount: 25,
		},
	}

	now := time.Now()
	for _, s := range seeds {
		p := &models.PointsMallProduct{
			ID: s.id, Name: s.name, Subtitle: s.subtitle, Description: s.description,
			PricePoints: s.price, CoverURL: s.cover, SortOrder: s.sort,
			Status: models.MallProductOnSale, CreatedAt: now, UpdatedAt: now,
		}
		if err := r.db.Create(p).Error; err != nil {
			return err
		}
		codes := make([]models.PointsMallCode, 0, s.codeCount)
		for i := 1; i <= s.codeCount; i++ {
			codes = append(codes, models.PointsMallCode{
				ID:        tool.GenerateID(),
				ProductID: s.id,
				Code:      formatDemoCode(s.codePrefix, i),
				Status:    models.MallCodeAvailable,
				CreatedAt: now,
			})
		}
		if err := r.db.CreateInBatches(codes, 50).Error; err != nil {
			return err
		}
	}
	zap.L().Info("积分商城演示商品与激活码已写入")
	return nil
}

func formatDemoCode(prefix string, seq int) string {
	return fmt.Sprintf("%s-%04X-%04X-%04X", prefix, uint(seq*7919&0xFFFF), uint(seq*1301&0xFFFF), uint(seq*4177&0xFFFF))
}

func (r *PointsMallRepository) ListOnSaleProducts() ([]models.PointsMallProduct, error) {
	var list []models.PointsMallProduct
	err := r.db.Where("status = ?", models.MallProductOnSale).
		Order("sort_order ASC, id ASC").
		Find(&list).Error
	return list, err
}

func (r *PointsMallRepository) SearchProductsByName(q string, limit int) ([]models.PointsMallProduct, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []models.PointsMallProduct{}, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 5 {
		limit = 5
	}
	var list []models.PointsMallProduct
	err := r.db.Where("status = ? AND (name LIKE ? OR subtitle LIKE ?)",
		models.MallProductOnSale, "%"+q+"%", "%"+q+"%").
		Order("sort_order ASC, id ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

func (r *PointsMallRepository) GetProductByID(id uint64) (*models.PointsMallProduct, error) {
	var p models.PointsMallProduct
	err := r.db.Where("id = ? AND status = ?", id, models.MallProductOnSale).First(&p).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMallProductNotFound
		}
		return nil, err
	}
	return &p, nil
}

func (r *PointsMallRepository) CountAvailableCodes(productID uint64) (int64, error) {
	var n int64
	err := r.db.Model(&models.PointsMallCode{}).
		Where("product_id = ? AND status = ?", productID, models.MallCodeAvailable).
		Count(&n).Error
	return n, err
}

func (r *PointsMallRepository) SyncAllProductStocks(redisMall *RedisPointsMallRepository) error {
	if redisMall == nil {
		return nil
	}
	var ids []uint64
	if err := r.db.Model(&models.PointsMallProduct{}).
		Where("status = ?", models.MallProductOnSale).
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		n, err := r.CountAvailableCodes(id)
		if err != nil {
			return err
		}
		if err := redisMall.SetStock(id, n); err != nil {
			return err
		}
	}
	return nil
}

func (r *PointsMallRepository) GetOrderByID(orderID uint64) (*models.PointsMallOrder, error) {
	var o models.PointsMallOrder
	err := r.db.Where("order_id = ?", orderID).First(&o).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return &o, nil
}

func (r *PointsMallRepository) ListOrdersByUser(userID uint64, limit int) ([]models.PointsMallOrder, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var list []models.PointsMallOrder
	err := r.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// RedeemInTx 在 Redis 已扣库存后于 MySQL 内完成：占码、扣积分、写订单与流水。
func (r *PointsMallRepository) RedeemInTx(
	pointsDB *PointsRepository,
	userID uint64,
	product *models.PointsMallProduct,
	orderID uint64,
) (*models.PointsMallOrder, error) {
	var out *models.PointsMallOrder
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var code models.PointsMallCode
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("product_id = ? AND status = ?", product.ID, models.MallCodeAvailable).
			First(&code).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMallOutOfStock
			}
			return err
		}

		wallet, err := pointsDB.GetWalletForUpdate(tx, userID)
		if err != nil {
			if errors.Is(err, ErrWalletNotFound) {
				wallet, err = pointsDB.CreateWalletInTx(tx, userID)
				if err != nil {
					return err
				}
				wallet, err = pointsDB.GetWalletForUpdate(tx, userID)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}
		if wallet.Balance < product.PricePoints {
			return ErrInsufficientBalance
		}

		now := time.Now()
		soldAt := now
		if err := tx.Model(&models.PointsMallCode{}).
			Where("id = ? AND status = ?", code.ID, models.MallCodeAvailable).
			Updates(map[string]interface{}{
				"status":   models.MallCodeSold,
				"order_id": orderID,
				"buyer_id": userID,
				"sold_at":  soldAt,
			}).Error; err != nil {
			return err
		}

		txnID := tool.PointsStableTxnID(userID, "mall_redeem", orderID, product.ID)
		newBalance := wallet.Balance - product.PricePoints
		txn := &models.PointsTransaction{
			TxnID:        txnID,
			UserID:       userID,
			Amount:       -product.PricePoints,
			Type:         "expense",
			RefType:      "mall_redeem",
			RefID:        orderID,
			BalanceAfter: newBalance,
			Description:  "兑换：" + product.Name,
			CreatedAt:    now,
		}
		inserted, err := pointsDB.TryInsertTransaction(tx, txn)
		if err != nil {
			return err
		}
		if !inserted {
			return ErrPointsTxnIdempotentSkip
		}
		if err := pointsDB.UpdateBalance(tx, userID, -product.PricePoints, wallet.Version); err != nil {
			return err
		}

		order := &models.PointsMallOrder{
			OrderID:     orderID,
			UserID:      userID,
			ProductID:   product.ID,
			ProductName: product.Name,
			PointsSpent: product.PricePoints,
			Code:        code.Code,
			CreatedAt:   now,
		}
		if err := tx.Create(order).Error; err != nil {
			if isDuplicateKeyError(err) {
				return ErrPointsTxnIdempotentSkip
			}
			return err
		}
		out = order
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrPointsTxnIdempotentSkip) {
			existing, findErr := r.GetOrderByID(orderID)
			if findErr == nil {
				return existing, nil
			}
		}
		return nil, err
	}
	return out, nil
}
