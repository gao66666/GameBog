package service

import (
	"errors"
	"strconv"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/mq"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// PointsRules 积分规则：每次加分数 + 每日上限
var PointsRules = map[string]struct {
	Amount     int64
	DailyLimit int64
	Desc       string
}{
	"article":       {Amount: 10, DailyLimit: 50, Desc: "发文奖励"},
	"comment":       {Amount: 5, DailyLimit: 30, Desc: "评论奖励"},
	"article_liked": {Amount: 1, DailyLimit: 20, Desc: "文章被点赞"},
	"comment_liked": {Amount: 1, DailyLimit: 10, Desc: "评论被点赞"},
	"checkin":       {Amount: 2, DailyLimit: 2, Desc: "签到奖励"},
}

type PointsService struct {
	db          *gorm.DB
	pointsDB    *database.PointsRepository
	pointsRedis *database.RedisPointsRepository
}

func NewPointsService(db *gorm.DB, pointsDB *database.PointsRepository, pointsRedis *database.RedisPointsRepository) *PointsService {
	return &PointsService{
		db:          db,
		pointsDB:    pointsDB,
		pointsRedis: pointsRedis,
	}
}

// EarnPoints 获取积分（由业务层在业务成功后调用）。
// 幂等：txn_id 为流水表主键；事务内先插流水再改余额，冲突则视为已入账；Redis 日上限前用 TxnIDExists 避免重复消费多扣次数。
func (s *PointsService) EarnPoints(userID uint64, refType string, refID uint64) error {
	return s.earnPoints(userID, refType, refID, 0)
}

// EarnPointsWithTxnID 与 EarnPoints 相同，但指定稳定 txn_id（如 Kafka 消息携带）；传 0 则与 EarnPoints 一样现场生成 Snowflake。
func (s *PointsService) EarnPointsWithTxnID(userID uint64, refType string, refID uint64, txnID uint64) error {
	return s.earnPoints(userID, refType, refID, txnID)
}

func (s *PointsService) earnPoints(userID uint64, refType string, refID uint64, txnIDOrZero uint64) error {
	rule, ok := PointsRules[refType]
	if !ok {
		zap.L().Warn("未知积分类型", zap.String("ref_type", refType))
		return nil
	}

	txnID := txnIDOrZero
	if txnID == 0 {
		txnID = tool.GenerateID()
	}

	exists, err := s.pointsDB.TxnIDExists(txnID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	if _, err := s.pointsRedis.CheckAndIncrDailyLimit(userID, refType, rule.DailyLimit); err != nil {
		return err
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		wallet, err := s.pointsDB.GetWalletByUserIDTx(tx, userID)
		if err != nil {
			if err == database.ErrWalletNotFound {
				wallet = &models.UserWallet{UserID: userID}
				if createErr := tx.Create(wallet).Error; createErr != nil {
					return createErr
				}
				wallet, err = s.pointsDB.GetWalletByUserIDTx(tx, userID)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}

		newBalance := wallet.Balance + rule.Amount
		txn := &models.PointsTransaction{
			TxnID:        txnID,
			UserID:       userID,
			Amount:       rule.Amount,
			Type:         "income",
			RefType:      refType,
			RefID:        refID,
			BalanceAfter: newBalance,
			Description:  rule.Desc,
			CreatedAt:    time.Now(),
		}
		inserted, err := s.pointsDB.TryInsertTransaction(tx, txn)
		if err != nil {
			return err
		}
		if !inserted {
			return database.ErrPointsTxnIdempotentSkip
		}
		return s.pointsDB.UpdateBalance(tx, userID, rule.Amount, wallet.Version)
	}); err != nil {
		if errors.Is(err, database.ErrPointsTxnIdempotentSkip) {
			_ = s.pointsRedis.RollbackDailyLimit(userID, refType)
			return nil
		}
		_ = s.pointsRedis.RollbackDailyLimit(userID, refType)
		return err
	}

	return nil
}

// EnqueueEarn 统一入账入口：Kafka 开启则投递 points.earn，由消费端 EarnPointsWithTxnID 幂等落库；投递失败或未启用 Kafka 则同步入账。
// salt：区分同 ref 下多次合法入账（如点赞者 user_id）；无则传 0。ref_type 不在 PointsRules 内则直接忽略。
func (s *PointsService) EnqueueEarn(userID uint64, refType string, refID uint64, salt uint64) error {
	if _, ok := PointsRules[refType]; !ok {
		return nil
	}
	txnID := tool.PointsStableTxnID(userID, refType, refID, salt)
	if mq.PointsEarnKafkaAvailable() {
		err := mq.PublishPointsEarn(&mq.PointsEarnPayload{
			UserID:  userID,
			RefType: refType,
			RefID:   refID,
			TxnID:   txnID,
		})
		if err != nil {
			zap.L().Warn("积分入账 Kafka 投递失败，降级同步入账", zap.String("ref_type", refType), zap.Uint64("user_id", userID), zap.Error(err))
			return s.EarnPointsWithTxnID(userID, refType, refID, txnID)
		}
		return nil
	}
	return s.EarnPointsWithTxnID(userID, refType, refID, txnID)
}

// StartReconcileTicker 定期比对「流水 Σ amount」与钱包 balance，打日志兜底。
func (s *PointsService) StartReconcileTicker() {
	if s == nil || s.pointsDB == nil {
		return
	}
	go func() {
		tick := time.NewTicker(1 * time.Hour)
		defer tick.Stop()
		run := func() {
			rows, err := s.pointsDB.ListWalletLedgerMismatches(200)
			if err != nil {
				zap.L().Warn("积分对账查询失败", zap.Error(err))
				return
			}
			for _, row := range rows {
				zap.L().Warn("积分对账不一致",
					zap.Uint64("user_id", row.UserID),
					zap.Int64("wallet_balance", row.Balance),
					zap.Int64("ledger_sum", row.LedgerSum))
			}
		}
		run()
		for range tick.C {
			run()
		}
	}()
}

// GetWallet 获取用户积分账户
func (s *PointsService) GetWallet(userID uint64) (*models.UserWallet, error) {
	return s.pointsDB.GetWallet(userID)
}

// Checkin 签到：Bitmap 查重 → 标记 → 入账（经 EnqueueEarn，与业务发分同链）
func (s *PointsService) Checkin(userID uint64) error {
	ok, err := s.pointsRedis.HasCheckedIn(userID)
	if err != nil {
		return err
	}
	if ok {
		return tool.NewBizError(400, 40030, "今天已经签到过了")
	}

	if _, err := s.pointsRedis.MarkCheckin(userID); err != nil {
		return err
	}

	dayKey, err := strconv.ParseUint(time.Now().Format("20060102"), 10, 64)
	if err != nil {
		dayKey = 0
	}
	return s.EnqueueEarn(userID, "checkin", 0, dayKey)
}
