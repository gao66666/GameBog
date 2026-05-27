package service

import (
	"errors"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type PointsMallService struct {
	mallDB    *database.PointsMallRepository
	mallRedis *database.RedisPointsMallRepository
	pointsDB  *database.PointsRepository
	pointsRedis *database.RedisPointsRepository
}

func NewPointsMallService(
	mallDB *database.PointsMallRepository,
	mallRedis *database.RedisPointsMallRepository,
	pointsDB *database.PointsRepository,
	pointsRedis *database.RedisPointsRepository,
) *PointsMallService {
	return &PointsMallService{
		mallDB:      mallDB,
		mallRedis:   mallRedis,
		pointsDB:    pointsDB,
		pointsRedis: pointsRedis,
	}
}

func (s *PointsMallService) InitStockCache() {
	if s == nil || s.mallDB == nil {
		return
	}
	if err := s.mallDB.SyncAllProductStocks(s.mallRedis); err != nil {
		zap.L().Warn("积分商城 Redis 库存同步失败", zap.Error(err))
	}
}

func (s *PointsMallService) ListProducts() ([]models.PointsMallProductVO, error) {
	products, err := s.mallDB.ListOnSaleProducts()
	if err != nil {
		return nil, err
	}
	out := make([]models.PointsMallProductVO, 0, len(products))
	for _, p := range products {
		stock, err := s.stockForProduct(p.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, models.PointsMallProductVO{
			ID:          p.ID,
			Name:        p.Name,
			Subtitle:    p.Subtitle,
			Description: p.Description,
			PricePoints: p.PricePoints,
			CoverURL:    p.CoverURL,
			Stock:       stock,
			Status:      p.Status,
		})
	}
	return out, nil
}

func (s *PointsMallService) SearchProductsByName(q string, limit int) ([]models.PointsMallProductVO, error) {
	products, err := s.mallDB.SearchProductsByName(q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]models.PointsMallProductVO, 0, len(products))
	for _, p := range products {
		stock, err := s.stockForProduct(p.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, models.PointsMallProductVO{
			ID:          p.ID,
			Name:        p.Name,
			Subtitle:    p.Subtitle,
			Description: p.Description,
			PricePoints: p.PricePoints,
			CoverURL:    p.CoverURL,
			Stock:       stock,
			Status:      p.Status,
		})
	}
	return out, nil
}

func (s *PointsMallService) GetProduct(productID uint64) (*models.PointsMallProductVO, error) {
	product, err := s.mallDB.GetProductByID(productID)
	if err != nil {
		return nil, err
	}
	stock, err := s.stockForProduct(product.ID)
	if err != nil {
		return nil, err
	}
	vo := &models.PointsMallProductVO{
		ID:          product.ID,
		Name:        product.Name,
		Subtitle:    product.Subtitle,
		Description: product.Description,
		PricePoints: product.PricePoints,
		CoverURL:    product.CoverURL,
		Stock:       stock,
		Status:      product.Status,
	}
	return vo, nil
}

func (s *PointsMallService) stockForProduct(productID uint64) (int64, error) {
	if s.mallRedis != nil {
		n, err := s.mallRedis.GetStock(productID)
		if err != nil {
			return 0, err
		}
		if n >= 0 {
			return n, nil
		}
	}
	return s.mallDB.CountAvailableCodes(productID)
}

func (s *PointsMallService) ListMyOrders(userID uint64) ([]models.PointsMallOrder, error) {
	return s.mallDB.ListOrdersByUser(userID, 50)
}

func (s *PointsMallService) Redeem(userID, productID uint64, idempotencyKey string) (*models.PointsMallOrder, error) {
	if userID == 0 || productID == 0 {
		return nil, database.ErrMallProductNotFound
	}

	if idempotencyKey != "" && s.mallRedis != nil {
		if oid, hit, err := s.mallRedis.GetIdempotentOrderID(userID, idempotencyKey); err != nil {
			return nil, err
		} else if hit && oid != 0 {
			return s.GetOrderForUser(userID, oid)
		}
	}

	if s.mallRedis != nil {
		ok, err := s.mallRedis.AcquireRedeemLock(userID, 0)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, database.ErrMallRedeemBusy
		}
		defer s.mallRedis.ReleaseRedeemLock(userID)
	}

	product, err := s.mallDB.GetProductByID(productID)
	if err != nil {
		return nil, err
	}

	if s.mallRedis != nil {
		ok, err := s.mallRedis.TryDecrStock(productID)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, database.ErrMallOutOfStock
		}
	} else {
		n, err := s.mallDB.CountAvailableCodes(productID)
		if err != nil {
			return nil, err
		}
		if n <= 0 {
			return nil, database.ErrMallOutOfStock
		}
	}

	orderID := tool.GenerateID()
	order, err := s.mallDB.RedeemInTx(s.pointsDB, userID, product, orderID)
	if err != nil {
		if s.mallRedis != nil {
			_ = s.mallRedis.IncrStock(productID)
		}
		return nil, err
	}

	if idempotencyKey != "" && s.mallRedis != nil {
		_ = s.mallRedis.SaveIdempotentOrderID(userID, idempotencyKey, order.OrderID)
	}

	// 校正 Redis 与库内可用码数量（防止长期漂移）
	if n, cntErr := s.mallDB.CountAvailableCodes(productID); cntErr == nil {
		_ = s.mallRedis.SetStock(productID, n)
	}

	// 兑换后刷新 Redis 中的积分余额
	if s.pointsRedis != nil && s.pointsDB != nil {
		if wallet, werr := s.pointsDB.GetWallet(userID); werr == nil {
			_ = s.pointsRedis.SetWalletBalance(userID, wallet.Balance, wallet.FrozenBalance)
		}
	}

	return order, nil
}

func (s *PointsMallService) GetOrderForUser(userID, orderID uint64) (*models.PointsMallOrder, error) {
	order, err := s.mallDB.GetOrderByID(orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, database.ErrMallProductNotFound
		}
		return nil, err
	}
	if order.UserID != userID {
		return nil, database.ErrMallProductNotFound
	}
	return order, nil
}
