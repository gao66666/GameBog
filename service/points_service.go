package service

import (
	"context"
	"encoding/json"
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

// EarnPoints 获取积分（由业务层在业务成功后调用）
// - 检查 Redis 每日上限
// - 同一事务：插入 outbox（不直接更新余额，由 Kafka 消费者异步落账）
func (s *PointsService) EarnPoints(userID uint64, refType string, refID uint64) error {
	rule, ok := PointsRules[refType]
	if !ok {
		zap.L().Warn("未知积分类型", zap.String("ref_type", refType))
		return nil // 未知类型直接跳过，不影响主业务
	}

	// 1. Redis 检查每日上限
	if _, err := s.pointsRedis.CheckAndIncrDailyLimit(userID, refType, rule.DailyLimit); err != nil {
		return err
	}

	// 2. 同一事务插入 outbox
	outbox := &models.PointsOutbox{
		ID:          tool.GenerateID(),
		UserID:      userID,
		Amount:      rule.Amount,
		RefType:     refType,
		RefID:       refID,
		Description: rule.Desc,
		Status:      models.PointsOutboxStatusPending,
	}

	if err := s.db.Transaction(func(tx *gorm.DB) error {
		// 确保钱包存在
		_, err := s.pointsDB.GetWalletByUserIDTx(tx, userID)
		if err != nil {
			if err == database.ErrWalletNotFound {
				// 自动创建钱包
				wallet := &models.UserWallet{
					UserID: userID,
				}
				if createErr := tx.Create(wallet).Error; createErr != nil {
					return createErr
				}
			} else {
				return err
			}
		}
		return s.pointsDB.InsertOutbox(tx, outbox)
	}); err != nil {
		// 事务失败 → 回滚 Redis 计数器
		_ = s.pointsRedis.RollbackDailyLimit(userID, refType)
		return err
	}

	return nil
}

// ProcessPointsSettle Kafka 消费者处理积分结算
// 在事务中：更新余额（乐观锁）+ 记流水
func (s *PointsService) ProcessPointsSettleMessage(ctx context.Context, payload []byte) error {
	var msg mq.PointsSettleMsg
	if err := json.Unmarshal(payload, &msg); err != nil {
		zap.L().Error("积分消息反序列化失败", zap.Error(err))
		return err
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		// 1. 获取钱包（乐观锁读）
		wallet, err := s.pointsDB.GetWalletByUserIDTx(tx, msg.UserID)
		if err != nil {
			if err == database.ErrWalletNotFound {
				wallet = &models.UserWallet{UserID: msg.UserID}
				if createErr := tx.Create(wallet).Error; createErr != nil {
					return createErr
				}
				// 重新读一遍获得 version
				wallet, err = s.pointsDB.GetWalletByUserIDTx(tx, msg.UserID)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		}

		// 2. 更新余额（乐观锁）
		if err := s.pointsDB.UpdateBalance(tx, msg.UserID, msg.Amount, wallet.Version); err != nil {
			// 乐观锁冲突或余额不足 → 重试由 Kafka 重试机制保证
			return err
		}

		// 3. 计算新余额
		newBalance := wallet.Balance + msg.Amount

		// 4. 记录流水
		txn := &models.PointsTransaction{
			TxnID:        msg.TxnID,
			UserID:       msg.UserID,
			Amount:       msg.Amount,
			Type:         msg.Type,
			RefType:      msg.RefType,
			RefID:        msg.RefID,
			BalanceAfter: newBalance,
			Description:  msg.Description,
			CreatedAt:    time.Now(),
		}
		return s.pointsDB.InsertTransaction(tx, txn)
	})
}

// StartOutboxScanner 启动 Outbox 扫描器（后台 goroutine）
// 定期扫描 pending 消息，发送到 Kafka
func (s *PointsService) StartOutboxScanner() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		zap.L().Info("积分 Outbox 扫描器已启动")
		for range ticker.C {
			s.scanAndPublish()
		}
	}()
}

func (s *PointsService) scanAndPublish() {
	messages, err := s.pointsDB.GetPendingOutbox(50)
	if err != nil {
		zap.L().Error("获取待发送积分消息失败", zap.Error(err))
		return
	}
	if len(messages) == 0 {
		return
	}

	for _, msg := range messages {
		txnType := "income"
		if msg.Amount < 0 {
			txnType = "expense"
		}

		kafkaMsg := &mq.PointsSettleMsg{
			TxnID:       msg.ID,
			UserID:      msg.UserID,
			Amount:      msg.Amount,
			RefType:     msg.RefType,
			RefID:       msg.RefID,
			Type:        txnType,
			Description: msg.Description,
		}

		if err := mq.PublishPointsSettle(kafkaMsg); err != nil {
			zap.L().Error("发送积分消息到 Kafka 失败",
				zap.Uint64("outbox_id", msg.ID),
				zap.Error(err))
			_ = s.pointsDB.MarkOutboxFailed(msg.ID)
			continue
		}

		if err := s.pointsDB.MarkOutboxSent(msg.ID); err != nil {
			zap.L().Error("标记 Outbox 已发送失败",
				zap.Uint64("outbox_id", msg.ID),
				zap.Error(err))
		}
	}
}

// GetWallet 获取用户积分账户
func (s *PointsService) GetWallet(userID uint64) (*models.UserWallet, error) {
	return s.pointsDB.GetWallet(userID)
}

// Checkin 签到
// 1. Redis Bitmap 查重
// 2. 走 EarnPoints 流程
func (s *PointsService) Checkin(userID uint64) error {
	// 1. 查重
	ok, err := s.pointsRedis.HasCheckedIn(userID)
	if err != nil {
		return err
	}
	if ok {
		return tool.NewBizError(400, 40030, "今天已经签到过了")
	}

	// 2. 记录签到到 Redis Bitmap
	if _, err := s.pointsRedis.MarkCheckin(userID); err != nil {
		return err
	}

	// 3. 走积分流程
	if err := s.EarnPoints(userID, "checkin", 0); err != nil {
		return err
	}

	return nil
}
