package database

import (
	"errors"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrGameOutOfStock            = tool.NewBizError(400, 40041, "游戏已售罄")
	ErrInsufficientAccount       = tool.NewBizError(400, 40042, "账户余额不足")
)

var errGameStoreInvalidParam = tool.NewBizError(400, 40001, "不正确的参数")

type GameStoreRepository struct {
	db *gorm.DB
}

func NewGameStoreRepository(db *gorm.DB) *GameStoreRepository {
	return &GameStoreRepository{db: db}
}

func (r *GameStoreRepository) InitTable() error {
	if err := r.db.AutoMigrate(
		&models.GameLicenseCode{},
		&models.GameOrder{},
		&models.AccountTransaction{},
	); err != nil {
		return ErrInitGame
	}
	if err := r.seedLicensesForGamesWithoutStock(); err != nil {
		return err
	}
	zap.L().Info("游戏库库存与订单表初始化成功")
	return nil
}

func (r *GameStoreRepository) seedLicensesForGamesWithoutStock() error {
	var games []models.Game
	if err := r.db.Find(&games).Error; err != nil {
		return err
	}
	now := time.Now()
	for _, g := range games {
		if models.IsPaidGame(g.PriceCents) {
			var n int64
			if err := r.db.Model(&models.GameLicenseCode{}).
				Where("game_id = ? AND status = ?", g.ID, models.GameCodeAvailable).
				Count(&n).Error; err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			prefix := fmt.Sprintf("G%d", g.ID%100000)
			codes := make([]models.GameLicenseCode, 0, 20)
			for i := 1; i <= 20; i++ {
				codes = append(codes, models.GameLicenseCode{
					ID:        tool.GenerateID(),
					GameID:    g.ID,
					Code:      fmt.Sprintf("%s-%04X-%04X", prefix, uint(i*7919&0xFFFF), uint(i*4177&0xFFFF)),
					Status:    models.GameCodeAvailable,
					CreatedAt: now,
				})
			}
			if err := r.db.CreateInBatches(codes, 50).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *GameStoreRepository) CountAvailableLicenses(gameID uint64) (int64, error) {
	var n int64
	err := r.db.Model(&models.GameLicenseCode{}).
		Where("game_id = ? AND status = ?", gameID, models.GameCodeAvailable).
		Count(&n).Error
	return n, err
}

func (r *GameStoreRepository) ListOrdersByUser(userID uint64, limit int) ([]models.GameOrder, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	var list []models.GameOrder
	err := r.db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

func (r *GameStoreRepository) GetOrderByID(orderID uint64) (*models.GameOrder, error) {
	var o models.GameOrder
	err := r.db.Where("order_id = ?", orderID).First(&o).Error
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// PurchaseInTx 纯 MySQL 事务：占码（付费）、扣账户余额（付费）、写账户流水、写订单、写入游玩库。
func (r *GameStoreRepository) PurchaseInTx(
	userRepo *UserRepository,
	game *models.Game,
	userID uint64,
	orderID uint64,
) (*models.GameOrder, error) {
	if game == nil || game.ID == 0 || userID == 0 {
		return nil, errGameStoreInvalidParam
	}

	var out *models.GameOrder
	err := r.db.Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		code := "FREE"
		price := game.PriceCents
		if !models.IsFreeGame(price) {
			price = models.NormalizeGamePriceCents(price)
			// users.account_balance 为「元」整数；games.price_cents 为「分」
			chargeYuan := price / 100
			if chargeYuan <= 0 {
				chargeYuan = 1
			}

			var lic models.GameLicenseCode
			err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
				Where("game_id = ? AND status = ?", game.ID, models.GameCodeAvailable).
				First(&lic).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrGameOutOfStock
				}
				return err
			}
			code = lic.Code

			user, err := userRepo.GetUserForUpdate(tx, userID)
			if err != nil {
				return err
			}
			if user.AccountBalance < chargeYuan {
				return ErrInsufficientAccount
			}
			newBal := user.AccountBalance - chargeYuan
			res := tx.Model(&models.User{}).
				Where("id = ? AND account_balance >= ?", userID, chargeYuan).
				Update("account_balance", gorm.Expr("account_balance - ?", chargeYuan))
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return ErrInsufficientAccount
			}

			txnID := tool.PointsStableTxnID(userID, "game_purchase", orderID, game.ID)
			acctTxn := &models.AccountTransaction{
				TxnID:        txnID,
				UserID:       userID,
				Amount:       -chargeYuan,
				Type:         "expense",
				RefType:      "game_purchase",
				RefID:        orderID,
				BalanceAfter: newBal,
				Description:  "购买游戏：" + game.Name,
				CreatedAt:    now,
			}
			if err := tx.Create(acctTxn).Error; err != nil {
				if isDuplicateKeyError(err) {
					existing, findErr := r.GetOrderByID(orderID)
					if findErr == nil {
						out = existing
						return nil
					}
				}
				return err
			}

			soldAt := now
			if err := tx.Model(&models.GameLicenseCode{}).
				Where("id = ? AND status = ?", lic.ID, models.GameCodeAvailable).
				Updates(map[string]interface{}{
					"status":   models.GameCodeSold,
					"order_id": orderID,
					"buyer_id": userID,
					"sold_at":  soldAt,
				}).Error; err != nil {
				return err
			}
		}

		order := &models.GameOrder{
			OrderID:    orderID,
			UserID:     userID,
			GameID:     game.ID,
			GameName:   game.Name,
			PriceCents: price,
			Code:       code,
			CreatedAt:  now,
		}
		if err := tx.Create(order).Error; err != nil {
			if isDuplicateKeyError(err) {
				existing, findErr := r.GetOrderByID(orderID)
				if findErr == nil {
					out = existing
					return nil
				}
			}
			return err
		}

		ugp := &models.UserGamePlay{
			UserID: userID,
			GameID: game.ID,
		}
		if err := tx.Save(ugp).Error; err != nil {
			return err
		}

		out = order
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
