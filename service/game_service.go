package service

import (
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"gorm.io/gorm"
)

// GameDetailVO 游戏详情（含库存与是否已购）。
type GameDetailVO struct {
	Game      *models.Game `json:"game"`
	Stock     int64        `json:"stock"`
	Owned     bool         `json:"owned"`
	Free      bool         `json:"free"`
}

type GameService struct {
	gameRepo  *database.GameRepository
	gameStore *database.GameStoreRepository
	gameRedis *database.RedisGameRepository
	topicRepo *database.TopicRepository
	userRepo  *database.UserRepository
	userRedis *database.RedisUserRepository
}

var errGameInvalidParam = tool.NewBizError(400, 40001, "不正确的参数")

func NewGameService(
	gameRepo *database.GameRepository,
	gameStore *database.GameStoreRepository,
	gameRedis *database.RedisGameRepository,
	topicRepo *database.TopicRepository,
	userRepo *database.UserRepository,
	userRedis *database.RedisUserRepository,
) *GameService {
	return &GameService{
		gameRepo:  gameRepo,
		gameStore: gameStore,
		gameRedis: gameRedis,
		topicRepo: topicRepo,
		userRepo:  userRepo,
		userRedis: userRedis,
	}
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
	if _, err := s.ensureGameTopicAfterCreate(game.ID, game.Name); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.InvalidateGamesListCache()
	}
	return nil
}

// CreateGameWithTopic 创建游戏并绑定「游戏:名称」话题，返回话题 ID。
func (s *GameService) CreateGameWithTopic(game *models.Game) (uint, error) {
	if err := s.CreateGame(game); err != nil {
		return 0, err
	}
	if s.topicRepo == nil {
		return 0, nil
	}
	return s.topicRepo.GetTopicIDByGameID(game.ID)
}

func (s *GameService) ensureGameTopicAfterCreate(gameID uint64, gameName string) (uint, error) {
	if s.topicRepo == nil || gameID == 0 {
		return 0, nil
	}
	topicID, err := s.topicRepo.EnsureGameTopic(gameID, gameName)
	if err != nil {
		return 0, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteGameTopicMap(gameID)
	}
	return topicID, nil
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
		_ = s.gameRedis.InvalidateGamesListCache()
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
		_ = s.gameRedis.DeleteGameTopicMap(id)
		_ = s.gameRedis.InvalidateGamesListCache()
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
	if s.gameRedis != nil {
		if list, total, hit, err := s.gameRedis.GetGamesList(page, size); err == nil && hit {
			return list, total, nil
		}
	}
	list, total, err := s.gameRepo.ListGames(page, size)
	if err != nil {
		return nil, 0, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetGamesList(page, size, list, total)
	}
	return list, total, nil
}

func (s *GameService) SearchGamesByName(q string, limit int) ([]*models.Game, error) {
	return s.gameRepo.SearchGamesByName(q, limit)
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
	if _, err := s.gameRepo.GetReviewByGameAndUser(review.GameID, review.UserID); err == nil {
		return tool.NewBizError(400, 40001, "您已对该游戏发表过点评，请编辑原点评")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
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

// --- UserGamePlay ---

func (s *GameService) UpsertUserGamePlay(ugp *models.UserGamePlay) error {
	if ugp == nil || ugp.UserID == 0 || ugp.GameID == 0 {
		return errGameInvalidParam
	}
	if err := s.gameRepo.UpsertUserGamePlay(ugp); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteUserGamePlay(ugp.UserID, ugp.GameID)
		_ = s.gameRedis.DeleteUserGamePlayList(ugp.UserID)
	}
	return nil
}

func (s *GameService) GetUserGamePlay(userID, gameID uint64) (*models.UserGamePlay, error) {
	if userID == 0 || gameID == 0 {
		return nil, errGameInvalidParam
	}
	if s.gameRedis != nil {
		if cached, hit, err := s.gameRedis.GetUserGamePlay(userID, gameID); err == nil && hit {
			return cached, nil
		}
	}
	ugp, err := s.gameRepo.GetUserGamePlay(userID, gameID)
	if err != nil {
		return nil, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetUserGamePlay(ugp)
	}
	return ugp, nil
}

func (s *GameService) ListUserGamePlays(userID uint64) ([]*models.UserGamePlay, error) {
	if userID == 0 {
		return nil, errGameInvalidParam
	}
	if s.gameRedis != nil {
		if cached, hit, err := s.gameRedis.GetUserGamePlayList(userID); err == nil && hit {
			return cached, nil
		}
	}
	list, err := s.gameRepo.ListUserGamePlays(userID)
	if err != nil {
		return nil, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetUserGamePlayList(userID, list)
	}
	return list, nil
}

func (s *GameService) DeleteUserGamePlay(userID, gameID uint64) error {
	if userID == 0 || gameID == 0 {
		return errGameInvalidParam
	}
	if err := s.gameRepo.DeleteUserGamePlay(userID, gameID); err != nil {
		return err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteUserGamePlay(userID, gameID)
		_ = s.gameRedis.DeleteUserGamePlayList(userID)
	}
	return nil
}

// GetGameTopicID 获取游戏对应的话题 ID（走 Redis 缓存穿透）。
func (s *GameService) GetGameTopicID(gameID uint64) (uint, error) {
	if gameID == 0 {
		return 0, errGameInvalidParam
	}
	if s.gameRedis != nil {
		if cached, hit, err := s.gameRedis.GetGameTopicMap(gameID); err == nil && hit {
			return cached, nil
		}
	}
	topicID, err := s.topicRepo.GetTopicIDByGameID(gameID)
	if err != nil {
		return 0, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetGameTopicMap(gameID, topicID)
	}
	return topicID, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func NewNotFoundError(msg string) error {
	return tool.NewBizError(404, 40001, msg)
}

func (s *GameService) StockForGame(gameID uint64, priceCents int64) (int64, error) {
	if s.gameStore == nil {
		return 0, nil
	}
	if models.IsFreeGame(priceCents) {
		return -1, nil // 免费不限库存
	}
	return s.gameStore.CountAvailableLicenses(gameID)
}

func (s *GameService) GetGameDetailForUser(gameID, userID uint64) (*GameDetailVO, error) {
	game, err := s.GetGameByID(gameID)
	if err != nil {
		return nil, err
	}
	stock, err := s.StockForGame(game.ID, game.PriceCents)
	if err != nil {
		return nil, err
	}
	return &GameDetailVO{
		Game:  game,
		Stock: stock,
		Owned: false,
		Free:  models.IsFreeGame(game.PriceCents),
	}, nil
}

func (s *GameService) PurchaseGame(userID, gameID uint64, idempotencyKey string) (*models.GameOrder, error) {
	if s.gameStore == nil || s.userRepo == nil {
		return nil, errGameInvalidParam
	}
	game, err := s.GetGameByID(gameID)
	if err != nil {
		return nil, err
	}
	orderID := tool.GenerateID()
	if idempotencyKey != "" {
		// 稳定 order_id 便于幂等重试
		orderID = tool.PointsStableTxnID(userID, "game_purchase_idem", gameID, hashIdem(idempotencyKey))
	}
	order, err := s.gameStore.PurchaseInTx(s.userRepo, game, userID, orderID)
	if err != nil {
		return nil, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.DeleteUserGamePlay(userID, gameID)
		_ = s.gameRedis.DeleteUserGamePlayList(userID)
		_ = s.gameRedis.DelUserGameOrders(userID)
	}
	if s.userRedis != nil {
		_ = s.userRedis.DelAccountBalance(userID)
	}
	return order, nil
}

func (s *GameService) ListMyGameOrders(userID uint64) ([]models.GameOrder, error) {
	if s.gameStore == nil {
		return []models.GameOrder{}, nil
	}
	if s.gameRedis != nil {
		if cached, hit, err := s.gameRedis.GetUserGameOrders(userID); err == nil && hit {
			return cached, nil
		}
	}
	list, err := s.gameStore.ListOrdersByUser(userID, 50)
	if err != nil {
		return nil, err
	}
	if s.gameRedis != nil {
		_ = s.gameRedis.SetUserGameOrders(userID, list)
	}
	return list, nil
}

func hashIdem(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
