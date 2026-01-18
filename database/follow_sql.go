package database

import (
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
