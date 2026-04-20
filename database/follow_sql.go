package database

import (
	"strings"

	"github.com/gao66666/GoBlog/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
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
	if err := r.db.AutoMigrate(&models.Follow{}); err != nil {
		zap.L().Error("关注表初始化失败", zap.Error(err))
		return ErrInitFollow
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
