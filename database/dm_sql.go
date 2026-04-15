package database

import (
	"time"

	"github.com/gao66666/GoBlog/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type DMRepository struct {
	db *gorm.DB
}

func NewDMRepository(db *gorm.DB) *DMRepository {
	return &DMRepository{db: db}
}

func (r *DMRepository) InitTable() error {
	if err := r.db.AutoMigrate(&models.DirectMessage{}); err != nil {
		zap.L().Error("私信表初始化失败", zap.Error(err))
		return err
	}
	zap.L().Info("私信表初始化成功")
	return nil
}

func (r *DMRepository) CreateMessage(m *models.DirectMessage) error {
	if m == nil {
		return gorm.ErrInvalidData
	}
	return r.db.Create(m).Error
}

func (r *DMRepository) ListMessagesBetween(userID, peerID uint64, limit int) ([]*models.DirectMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	now := time.Now()
	var out []*models.DirectMessage
	// 取最新 N 条（倒序），再由 service 反转成时间正序
	err := r.db.Model(&models.DirectMessage{}).
		Where("expire_at > ?", now).
		Where(r.db.Where("from_user_id = ? AND to_user_id = ?", userID, peerID).
			Or("from_user_id = ? AND to_user_id = ?", peerID, userID)).
		Order("sent_at DESC").
		Limit(limit).
		Find(&out).Error
	return out, err
}

// ListPeers 返回 userID 参与的会话对象列表（按最新消息时间倒序）。
func (r *DMRepository) ListPeers(userID uint64, limit int) ([]uint64, error) {
	if limit <= 0 {
		limit = 50
	}
	now := time.Now()

	// MySQL: 用子查询找每个 peer 的 latest_sent_at，再排序取前 N。
	// peer_id = CASE WHEN from_user_id=userID THEN to_user_id ELSE from_user_id END
	rows, err := r.db.Raw(`
SELECT peer_id
FROM (
  SELECT
    CASE WHEN from_user_id = ? THEN to_user_id ELSE from_user_id END AS peer_id,
    MAX(sent_at) AS latest_sent_at
  FROM direct_messages
  WHERE expire_at > ? AND (from_user_id = ? OR to_user_id = ?)
  GROUP BY peer_id
) t
ORDER BY latest_sent_at DESC
LIMIT ?
`, userID, now, userID, userID, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	peers := make([]uint64, 0)
	for rows.Next() {
		var pid uint64
		if scanErr := rows.Scan(&pid); scanErr != nil {
			return nil, scanErr
		}
		if pid != 0 {
			peers = append(peers, pid)
		}
	}
	return peers, nil
}

func (r *DMRepository) DeleteExpired(before time.Time) error {
	return r.db.Where("expire_at <= ?", before).Delete(&models.DirectMessage{}).Error
}
