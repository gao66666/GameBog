package database

import (
	"errors"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ErrPointsTxnIdempotentSkip 流水主键 txn_id 已存在：本笔为重复请求/重复消费，余额不应再次变更。
var ErrPointsTxnIdempotentSkip = errors.New("points txn idempotent skip")

var (
	ErrInitPoints           = tool.NewBizError(500, 50010, "积分表初始化失败")
	ErrWalletNotFound       = tool.NewBizError(404, 40020, "积分账户不存在")
	ErrInsufficientBalance  = tool.NewBizError(400, 40021, "积分余额不足")
	ErrConcurrentModify     = tool.NewBizError(409, 40022, "积分账户并发修改，请重试")
	ErrDailyLimitExceeded   = tool.NewBizError(400, 40024, "今日积分获取已达上限")
)

// InitialWalletBalance 新用户首次创建积分账户时的赠送余额。
const InitialWalletBalance int64 = 100

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

// CreateWallet 创建用户积分账户并写入初始赠送流水（幂等：已存在则跳过）。
func (r *PointsRepository) CreateWallet(userID uint64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		_, err := r.CreateWalletInTx(tx, userID)
		return err
	})
}

// CreateWalletInTx 在事务内创建钱包 + 初始积分流水（已存在则返回现有账户）。
func (r *PointsRepository) CreateWalletInTx(tx *gorm.DB, userID uint64) (*models.UserWallet, error) {
	var existing models.UserWallet
	err := tx.Where("user_id = ?", userID).First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	now := time.Now()
	wallet := &models.UserWallet{
		UserID:        userID,
		Balance:       InitialWalletBalance,
		FrozenBalance: 0,
		Version:       0,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := tx.Create(wallet).Error; err != nil {
		if isDuplicateKeyError(err) {
			return r.GetWalletByUserIDTx(tx, userID)
		}
		return nil, err
	}

	txnID := tool.PointsStableTxnID(userID, "initial_grant", userID, 0)
	txn := &models.PointsTransaction{
		TxnID:        txnID,
		UserID:       userID,
		Amount:       InitialWalletBalance,
		Type:         "income",
		RefType:      "initial_grant",
		RefID:        userID,
		BalanceAfter: InitialWalletBalance,
		Description:  "新用户初始积分",
		CreatedAt:    now,
	}
	if err := tx.Create(txn).Error; err != nil && !isDuplicateKeyError(err) {
		return nil, err
	}
	return wallet, nil
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

// InsertTransaction 插入积分流水记录（遇主键冲突返回错误；幂等场景请用 TryInsertTransaction）。
func (r *PointsRepository) InsertTransaction(tx *gorm.DB, txn *models.PointsTransaction) error {
	return tx.Create(txn).Error
}

// TxnIDExists 是否已有该 txn_id 流水（用于在 Redis 日上限之前短路，避免重复消费多扣次数）。
func (r *PointsRepository) TxnIDExists(txnID uint64) (bool, error) {
	if txnID == 0 {
		return false, nil
	}
	var row models.PointsTransaction
	err := r.db.Select("txn_id").Where("txn_id = ?", txnID).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// TryInsertTransaction 插入流水；主键 txn_id 冲突时返回 inserted=false（无错误），供与 UpdateBalance 同事务幂等。
func (r *PointsRepository) TryInsertTransaction(tx *gorm.DB, txn *models.PointsTransaction) (inserted bool, err error) {
	if err := tx.Create(txn).Error; err != nil {
		if isDuplicateKeyError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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

// PointsWalletLedgerMismatch 钱包余额与流水 Σ(amount) 不一致（当前流水均为收入，Σ 应与 balance 对齐）。
type PointsWalletLedgerMismatch struct {
	UserID    uint64 `gorm:"column:user_id"`
	Balance   int64  `gorm:"column:balance"`
	LedgerSum int64  `gorm:"column:ledger_sum"`
}

// ListTransactionsByUser 按 user_id 分页查询积分流水（按时间倒序）。
func (r *PointsRepository) ListTransactionsByUser(userID uint64, page, size int) ([]models.PointsTransaction, int64, error) {
	if userID == 0 {
		return []models.PointsTransaction{}, 0, nil
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 50 {
		size = 50
	}

	tbl := (&models.PointsTransaction{}).TableName()
	q := r.db.Table(tbl).Where("user_id = ?", userID)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	list := make([]models.PointsTransaction, 0)
	err := r.db.Table(tbl).
		Where("user_id = ?", userID).
		Order("created_at DESC, txn_id DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&list).Error
	if err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListWalletLedgerMismatches 对账抽样：仅返回前 limit 条不一致用户。
func (r *PointsRepository) ListWalletLedgerMismatches(limit int) ([]PointsWalletLedgerMismatch, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []PointsWalletLedgerMismatch
	err := r.db.Raw(`
SELECT w.user_id, w.balance, COALESCE(SUM(t.amount), 0) AS ledger_sum
FROM user_wallets w
LEFT JOIN points_transactions t ON t.user_id = w.user_id
GROUP BY w.user_id, w.balance
HAVING w.balance <> COALESCE(SUM(t.amount), 0)
LIMIT ?`, limit).Scan(&rows).Error
	return rows, err
}
