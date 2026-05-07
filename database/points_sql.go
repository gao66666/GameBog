package database

import (
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrInitPoints           = tool.NewBizError(500, 50010, "积分表初始化失败")
	ErrWalletNotFound       = tool.NewBizError(404, 40020, "积分账户不存在")
	ErrInsufficientBalance  = tool.NewBizError(400, 40021, "积分余额不足")
	ErrConcurrentModify     = tool.NewBizError(409, 40022, "积分账户并发修改，请重试")
	ErrOutboxNotFound       = tool.NewBizError(404, 40023, "积分发件箱消息不存在")
	ErrDailyLimitExceeded   = tool.NewBizError(400, 40024, "今日积分获取已达上限")
)

type PointsRepository struct {
	db *gorm.DB
}

func NewPointsRepository(db *gorm.DB) *PointsRepository {
	return &PointsRepository{db: db}
}

func (r *PointsRepository) InitTable() error {
	if err := r.db.AutoMigrate(
		&models.UserWallet{},
		&models.PointsTransaction{},
		&models.PointsOutbox{},
	); err != nil {
		return ErrInitPoints
	}
	zap.L().Info("积分相关表初始化成功")
	return nil
}

// GetWallet 获取用户积分账户
func (r *PointsRepository) GetWallet(userID uint64) (*models.UserWallet, error) {
	var wallet models.UserWallet
	err := r.db.Where("user_id = ?", userID).First(&wallet).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return &wallet, nil
}

// GetWalletForUpdate 获取用户积分账户（带行锁，用于事务内扣减前查询）
func (r *PointsRepository) GetWalletForUpdate(tx *gorm.DB, userID uint64) (*models.UserWallet, error) {
	var wallet models.UserWallet
	err := tx.Set("gorm:query_option", "FOR UPDATE").Where("user_id = ?", userID).First(&wallet).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return &wallet, nil
}

// CreateWallet 创建用户积分账户
func (r *PointsRepository) CreateWallet(userID uint64) error {
	wallet := &models.UserWallet{
		UserID:        userID,
		Balance:       0,
		FrozenBalance: 0,
		Version:       0,
	}
	if err := r.db.Create(wallet).Error; err != nil {
		return err
	}
	return nil
}

// UpdateBalance 乐观锁更新余额（扣减时需确保 balance >= 0）
// amount 正数=增加，负数=扣减
func (r *PointsRepository) UpdateBalance(tx *gorm.DB, userID uint64, amount int64, version int) error {
	expr := gorm.Expr("balance + ?", amount)
	// 如果扣减余额（amount < 0），要求扣减后 >= 0
	where := "user_id = ? AND version = ?"
	args := []interface{}{userID, version}
	if amount < 0 {
		where += " AND balance >= ?"
		args = append(args, -amount)
	}

	result := tx.Model(&models.UserWallet{}).
		Where(where, args...).
		Updates(map[string]interface{}{
			"balance":    expr,
			"version":    version + 1,
			"updated_at": time.Now(),
		})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 检查是 version 冲突还是余额不足
		var cur models.UserWallet
		if err := tx.Where("user_id = ?", userID).First(&cur).Error; err == nil {
			if cur.Version != version {
				return ErrConcurrentModify
			}
			if cur.Balance < -amount {
				return ErrInsufficientBalance
			}
		}
		return ErrConcurrentModify
	}
	return nil
}

// InsertTransaction 插入积分流水记录
func (r *PointsRepository) InsertTransaction(tx *gorm.DB, txn *models.PointsTransaction) error {
	return tx.Create(txn).Error
}

// InsertOutbox 插入积分发件箱消息（与业务操作在同一事务）
func (r *PointsRepository) InsertOutbox(tx *gorm.DB, outbox *models.PointsOutbox) error {
	return tx.Create(outbox).Error
}

// GetPendingOutbox 获取待发送的积分消息
func (r *PointsRepository) GetPendingOutbox(limit int) ([]models.PointsOutbox, error) {
	var outbox []models.PointsOutbox
	err := r.db.Where("status = ?", models.PointsOutboxStatusPending).
		Order("id ASC").
		Limit(limit).
		Find(&outbox).Error
	if err != nil {
		return nil, err
	}
	return outbox, nil
}

// MarkOutboxSent 标记发件箱消息为已发送
func (r *PointsRepository) MarkOutboxSent(id uint64) error {
	return r.db.Model(&models.PointsOutbox{}).
		Where("id = ?", id).
		Update("status", models.PointsOutboxStatusSent).Error
}

// MarkOutboxFailed 标记发件箱消息为失败
func (r *PointsRepository) MarkOutboxFailed(id uint64) error {
	return r.db.Model(&models.PointsOutbox{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":      models.PointsOutboxStatusFailed,
			"retry_count": gorm.Expr("retry_count + 1"),
		}).Error
}

// GetWalletByUserIDTx 事务内获取钱包（乐观读取，不带锁）
func (r *PointsRepository) GetWalletByUserIDTx(tx *gorm.DB, userID uint64) (*models.UserWallet, error) {
	var wallet models.UserWallet
	err := tx.Where("user_id = ?", userID).First(&wallet).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrWalletNotFound
		}
		return nil, err
	}
	return &wallet, nil
}
