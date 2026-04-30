package database

import (
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrInitGame = tool.NewBizError(404, 40001, "游戏相关数据库初始化出错")
)

type GameRepository struct {
	db *gorm.DB
}

func NewGameRepository(db *gorm.DB) *GameRepository {
	return &GameRepository{db: db}
}

// InitTable 初始化游戏相关表结构
func (r *GameRepository) InitTable() error {
	if err := r.db.AutoMigrate(
		&models.Game{},
		&models.GameReview{},
		&models.GameReviewComment{},
	); err != nil {
		return ErrInitGame
	}
	zap.L().Info("游戏相关表初始化成功")
	return nil
}

func (r *GameRepository) CreateGame(game *models.Game) error {
	return r.db.Create(game).Error
}

func (r *GameRepository) UpdateGame(game *models.Game) error {
	return r.db.Save(game).Error
}

func (r *GameRepository) DeleteGame(id uint64) error {
	return r.db.Delete(&models.Game{}, "id = ?", id).Error
}

func (r *GameRepository) GetGameByID(id uint64) (*models.Game, error) {
	var game models.Game
	if err := r.db.First(&game, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &game, nil
}

func (r *GameRepository) ListGames(page, size int) ([]*models.Game, int64, error) {
	var (
		list  []*models.Game
		total int64
	)
	if err := r.db.Model(&models.Game{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*models.Game{}, 0, nil
	}
	offset := (page - 1) * size
	if err := r.db.Order("created_at DESC").Offset(offset).Limit(size).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *GameRepository) CreateReview(review *models.GameReview) error {
	return r.db.Create(review).Error
}

func (r *GameRepository) UpdateReview(review *models.GameReview) error {
	return r.db.Save(review).Error
}

func (r *GameRepository) DeleteReview(id uint64) error {
	return r.db.Delete(&models.GameReview{}, "id = ?", id).Error
}

func (r *GameRepository) GetReviewByID(id uint64) (*models.GameReview, error) {
	var review models.GameReview
	if err := r.db.First(&review, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &review, nil
}

func (r *GameRepository) ListReviewsByGameID(gameID uint64, page, size int) ([]*models.GameReview, int64, error) {
	var (
		list  []*models.GameReview
		total int64
	)
	if err := r.db.Model(&models.GameReview{}).Where("game_id = ?", gameID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []*models.GameReview{}, 0, nil
	}
	offset := (page - 1) * size
	if err := r.db.Where("game_id = ?", gameID).Order("reviewed_at DESC, created_at DESC").Offset(offset).Limit(size).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *GameRepository) CreateReviewComment(comment *models.GameReviewComment) error {
	return r.db.Create(comment).Error
}

func (r *GameRepository) UpdateReviewComment(comment *models.GameReviewComment) error {
	return r.db.Save(comment).Error
}

func (r *GameRepository) DeleteReviewComment(id uint64) error {
	return r.db.Delete(&models.GameReviewComment{}, "id = ?", id).Error
}

func (r *GameRepository) GetReviewCommentByID(id uint64) (*models.GameReviewComment, error) {
	var comment models.GameReviewComment
	if err := r.db.First(&comment, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *GameRepository) ListReviewComments(reviewID uint64, page, size int) ([]*models.GameReviewComment, int64, error) {
