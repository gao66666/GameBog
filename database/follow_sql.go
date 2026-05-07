package database

import (
	"strings"

	"github.com/gao66666/GoBlog/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FollowRepository struct {
	db *gorm.DB
}

func NewFollowRepository(db *gorm.DB) *FollowRepository {
	return &FollowRepository{db: db}
}

// 初始化表结构
// InitTable 初始化关注表结构
func (r *FollowRepository) InitTable() error {
	if err := r.db.AutoMigrate(&models.Follow{}, &models.TopicFollow{}); err != nil {
		zap.L().Error("关注表初始化失败", zap.Error(err))
		return ErrInitFollow
	}
	// 历史表可能仍有 game_id；模型已移除，需删掉该列否则 INSERT 会失败
	if r.db.Migrator().HasTable(&models.TopicFollow{}) && r.db.Migrator().HasColumn(&models.TopicFollow{}, "game_id") {
		if err := r.db.Migrator().DropColumn(&models.TopicFollow{}, "game_id"); err != nil {
			zap.L().Warn("移除 topic_follows.game_id 失败（可手工执行 migrations/000002_topic_follow_drop_game_id.sql）", zap.Error(err))
		} else {
			zap.L().Info("已移除 topic_follows.game_id 遗留列")
		}
	}
	zap.L().Info("关注表初始化成功")
	return nil
}

// Follow 关注用户
func (r *FollowRepository) CreateFollow(param *models.ParamFollow) error {
	follow := &models.Follow{
		FollowerID:  param.FollowerID,
		FollowingID: param.FollowingID,
	}

	// 4. 插入数据库
	if err := r.db.Create(follow).Error; err != nil {
		// 判断是否是重复关注（唯一索引冲突）
		if strings.Contains(err.Error(), "Duplicate entry") ||
			strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return ErrDoubleFollow
		}
		zap.L().Error("关注用户失败",
			zap.Uint64("follower_id", param.FollowerID),
			zap.Uint64("following_id", param.FollowingID),
			zap.Error(err))
		return ErrSQLFollow
	}

	return nil
}

// GetFollowingIDs 查询我关注的用户ID列表。
func (r *FollowRepository) GetFollowingIDs(followerID uint64, limit int) ([]uint64, error) {
	if followerID == 0 {
		return []uint64{}, nil
	}
	if limit <= 0 {
		limit = 5000
	}
	ids := make([]uint64, 0)
	err := r.db.Model(&models.Follow{}).
		Where("follower_id = ?", followerID).
		Order("created_at DESC").
		Limit(limit).
		Pluck("following_id", &ids).Error
	return ids, err
}

// CountFollowingUsers 当前用户关注的「用户」数量（follows 表，非话题关注）。
func (r *FollowRepository) CountFollowingUsers(followerID uint64) (int64, error) {
	if followerID == 0 {
		return 0, nil
	}
	var n int64
	err := r.db.Model(&models.Follow{}).
		Where("follower_id = ?", followerID).
		Count(&n).Error
	return n, err
}

// --- TopicFollow ---

func (r *FollowRepository) CreateTopicFollow(userID uint64, topicID uint) error {
	tf := &models.TopicFollow{UserID: userID, TopicID: topicID}
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(tf).Error
}

func (r *FollowRepository) DeleteTopicFollow(userID uint64, topicID uint) error {
	return r.db.Where("user_id = ? AND topic_id = ?", userID, topicID).Delete(&models.TopicFollow{}).Error
}

func (r *FollowRepository) ListTopicFollowByUser(userID uint64) ([]*models.TopicFollow, error) {
	var list []*models.TopicFollow
	err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *FollowRepository) ListUserIDsByTopicID(topicID uint) ([]uint64, error) {
	var ids []uint64
	err := r.db.Model(&models.TopicFollow{}).
		Where("topic_id = ?", topicID).
		Pluck("user_id", &ids).Error
	return ids, err
}

func (r *FollowRepository) IsTopicFollowed(userID uint64, topicID uint) (bool, error) {
	var count int64
	err := r.db.Model(&models.TopicFollow{}).
		Where("user_id = ? AND topic_id = ?", userID, topicID).
		Count(&count).Error
	return count > 0, err
}
