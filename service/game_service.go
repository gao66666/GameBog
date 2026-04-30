package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"gorm.io/gorm"
)

type GameService struct {
	gameRepo  *database.GameRepository
	gameRedis *database.RedisGameRepository
	topicRepo *database.TopicRepository
}

var errGameInvalidParam = tool.NewBizError(400, 40001, "不正确的参数")

func NewGameService(gameRepo *database.GameRepository, gameRedis *database.RedisGameRepository, topicRepo *database.TopicRepository) *GameService {
	return &GameService{gameRepo: gameRepo, gameRedis: gameRedis, topicRepo: topicRepo}
}

func (s *GameService) CreateGame(game *models.Game) error {
	if game == nil {
		return errGameInvalidParam
	}
	game.Name = strings.TrimSpace(game.Name)
	if game.Name == "" {
		return errGameInvalidParam
	}
	if err := s.gameRepo.CreateGame(game); err != nil {
		return err
	}
	if s.topicRepo != nil {
		if _, err := s.topicRepo.EnsureGameTopic(game.ID, game.Name); err != nil {
			return err
		}
	}
	return nil
}

func (s *GameService) UpdateGame(game *models.Game) error {
	if game == nil || game.ID == 0 {
		return errGameInvalidParam
	}
	game.Name = strings.TrimSpace(game.Name)
	if game.Name == "" {
		return errGameInvalidParam
	}
	if err := s.gameRepo.UpdateGame(game); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteKey(fmt.Sprintf("game:detail:%d", game.ID))
	}
	return nil
}

func (s *GameService) DeleteGame(id uint64) error {
	if id == 0 {
		return errGameInvalidParam
	}
	if err := s.gameRepo.DeleteGame(id); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteKey(fmt.Sprintf("game:detail:%d", id))
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:reviews:list:%d:*", id))
	}
	return nil
}

func (s *GameService) GetGameByID(id uint64) (*models.Game, error) {
	if id == 0 {
		return nil, errGameInvalidParam
	}
	if s.gameRedis != nil {
		if cached, hit, err := s.gameRedis.GetGame(id); err == nil && hit {
			return cached, nil
		}
	}
	game, err := s.gameRepo.GetGameByID(id)
	if err != nil {
		return nil, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetGame(game)
	}
	return game, nil
}

func (s *GameService) ListGames(page, size int) ([]*models.Game, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	return s.gameRepo.ListGames(page, size)
}

func (s *GameService) CreateReview(review *models.GameReview) error {
	if review == nil || review.GameID == 0 || review.UserID == 0 {
		return errGameInvalidParam
	}
	review.Content = strings.TrimSpace(review.Content)
	if review.Content == "" || review.Rating < 1 || review.Rating > 5 {
		return errGameInvalidParam
	}
	if review.ReviewedAt.IsZero() {
		review.ReviewedAt = time.Now()
	}
	if err := s.gameRepo.CreateReview(review); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:reviews:list:%d:*", review.GameID))
	}
	return nil
}

func (s *GameService) UpdateReview(review *models.GameReview) error {
	if review == nil || review.ID == 0 {
		return errGameInvalidParam
	}
	review.Content = strings.TrimSpace(review.Content)
	if review.Content == "" || review.Rating < 1 || review.Rating > 5 {
		return errGameInvalidParam
	}
	if review.ReviewedAt.IsZero() {
		review.ReviewedAt = time.Now()
	}
	if err := s.gameRepo.UpdateReview(review); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteKey(fmt.Sprintf("game:review:detail:%d", review.ID))
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:reviews:list:%d:*", review.GameID))
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:review:comments:%d:*", review.ID))
	}
	return nil
}

func (s *GameService) DeleteReview(id uint64) error {
	if id == 0 {
		return errGameInvalidParam
	}
	review, err := s.gameRepo.GetReviewByID(id)
	if err != nil {
		return err
	}
	if err := s.gameRepo.DeleteReview(id); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteKey(fmt.Sprintf("game:review:detail:%d", id))
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:reviews:list:%d:*", review.GameID))
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:review:comments:%d:*", id))
	}
	return nil
}

func (s *GameService) GetReviewByID(id uint64) (*models.GameReview, error) {
	if id == 0 {
		return nil, errGameInvalidParam
	}
	if s.gameRedis != nil {
		if cached, hit, err := s.gameRedis.GetReview(id); err == nil && hit {
			return cached, nil
		}
	}
	review, err := s.gameRepo.GetReviewByID(id)
	if err != nil {
		return nil, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetReview(review)
	}
	return review, nil
}

func (s *GameService) ListReviewsByGameID(gameID uint64, page, size int) ([]*models.GameReview, int64, error) {
	if gameID == 0 {
		return nil, 0, errGameInvalidParam
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	if s.gameRedis != nil {
		if cached, hit, err := s.gameRedis.GetReviewList(gameID, page, size); err == nil && hit {
			return cached, int64(len(cached)), nil
		}
	}
	list, total, err := s.gameRepo.ListReviewsByGameID(gameID, page, size)
	if err != nil {
		return nil, 0, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetReviewList(gameID, page, size, list)
	}
	return list, total, nil
}

func (s *GameService) CreateReviewComment(comment *models.GameReviewComment) error {
	if comment == nil || comment.ReviewID == 0 || comment.UserID == 0 {
		return errGameInvalidParam
	}
	comment.Content = strings.TrimSpace(comment.Content)
	if comment.Content == "" {
		return errGameInvalidParam
	}
	if err := s.gameRepo.CreateReviewComment(comment); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:review:comments:%d:*", comment.ReviewID))
	}
	return nil
}

func (s *GameService) GetReviewCommentByID(id uint64) (*models.GameReviewComment, error) {
	if id == 0 {
		return nil, errGameInvalidParam
	}
	return s.gameRepo.GetReviewCommentByID(id)
}

func (s *GameService) UpdateReviewComment(comment *models.GameReviewComment) error {
	if comment == nil || comment.ID == 0 {
		return errGameInvalidParam
	}
	comment.Content = strings.TrimSpace(comment.Content)
	if comment.Content == "" {
		return errGameInvalidParam
	}
	if err := s.gameRepo.UpdateReviewComment(comment); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:review:comments:%d:*", comment.ReviewID))
	}
	return nil
}

func (s *GameService) DeleteReviewComment(id uint64) error {
	if id == 0 {
		return errGameInvalidParam
	}
	comment, err := s.gameRepo.GetReviewCommentByID(id)
	if err != nil {
		return err
	}
	if err := s.gameRepo.DeleteReviewComment(id); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteKeysByPattern(fmt.Sprintf("game:review:comments:%d:*", comment.ReviewID))
	}
	return nil
}

func (s *GameService) ListReviewComments(reviewID uint64, page, size int) ([]*models.GameReviewComment, int64, error) {
	if reviewID == 0 {
		return nil, 0, errGameInvalidParam
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if s.gameRedis != nil {
		if cached, hit, err := s.gameRedis.GetReviewCommentList(reviewID, page, size); err == nil && hit {
			return cached, int64(len(cached)), nil
		}
	}
	list, total, err := s.gameRepo.ListReviewComments(reviewID, page, size)
	if err != nil {
		return nil, 0, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetReviewCommentList(reviewID, page, size, list)
	}
	return list, total, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func NewNotFoundError(msg string) error {
	return tool.NewBizError(404, 40001, msg)
}
