package database

import (
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var ErrInitPoints = tool.NewBizError(500, 50010, "积分表初始化失败")

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
