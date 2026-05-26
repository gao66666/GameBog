package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/redis/go-redis/v9"
)

const gameCacheTTL = 10 * time.Minute

func (r *RedisGameRepository) GetGame(id uint64) (*models.Game, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	val, err := r.client.Get(context.Background(), fmt.Sprintf("game:detail:%d", id)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var game models.Game
	if err := json.Unmarshal([]byte(val), &game); err != nil {
		return nil, false, err
	}
	return &game, true, nil
}

func (r *RedisGameRepository) SetGame(game *models.Game) error {
	if r == nil || r.client == nil || game == nil || game.ID == 0 {
		return nil
	}
	body, err := json.Marshal(game)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), fmt.Sprintf("game:detail:%d", game.ID), body, gameCacheTTL).Err()
}

func (r *RedisGameRepository) GetReview(id uint64) (*models.GameReview, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	val, err := r.client.Get(context.Background(), fmt.Sprintf("game:review:detail:%d", id)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var review models.GameReview
	if err := json.Unmarshal([]byte(val), &review); err != nil {
		return nil, false, err
	}
	return &review, true, nil
}

func (r *RedisGameRepository) SetReview(review *models.GameReview) error {
	if r == nil || r.client == nil || review == nil || review.ID == 0 {
		return nil
	}
	body, err := json.Marshal(review)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), fmt.Sprintf("game:review:detail:%d", review.ID), body, gameCacheTTL).Err()
}

func (r *RedisGameRepository) GetReviewList(gameID uint64, page, size int) ([]*models.GameReview, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	key := fmt.Sprintf("game:reviews:list:%d:p:%d:s:%d", gameID, page, size)
	val, err := r.client.Get(context.Background(), key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var list []*models.GameReview
	if err := json.Unmarshal([]byte(val), &list); err != nil {
		return nil, false, err
	}
	return list, true, nil
}

func (r *RedisGameRepository) SetReviewList(gameID uint64, page, size int, list []*models.GameReview) error {
	if r == nil || r.client == nil {
		return nil
	}
	key := fmt.Sprintf("game:reviews:list:%d:p:%d:s:%d", gameID, page, size)
	body, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), key, body, gameCacheTTL).Err()
}

func (r *RedisGameRepository) GetReviewCommentList(reviewID uint64, page, size int) ([]*models.GameReviewComment, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	key := fmt.Sprintf("game:review:comments:%d:p:%d:s:%d", reviewID, page, size)
	val, err := r.client.Get(context.Background(), key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var list []*models.GameReviewComment
	if err := json.Unmarshal([]byte(val), &list); err != nil {
		return nil, false, err
	}
	return list, true, nil
}

func (r *RedisGameRepository) SetReviewCommentList(reviewID uint64, page, size int, list []*models.GameReviewComment) error {
	if r == nil || r.client == nil {
		return nil
	}
	key := fmt.Sprintf("game:review:comments:%d:p:%d:s:%d", reviewID, page, size)
	body, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), key, body, gameCacheTTL).Err()
}

func (r *RedisGameRepository) DeleteKey(key string) error {
	if r == nil || r.client == nil || key == "" {
		return nil
	}
	return r.client.Del(context.Background(), key).Err()
}

func (r *RedisGameRepository) DeleteKeysByPattern(pattern string) error {
	if r == nil || r.client == nil || pattern == "" {
		return nil
	}
	return deleteKeysByPattern(r.client, pattern)
}

// --- UserGamePlay 缓存 ---

func (r *RedisGameRepository) GetUserGamePlay(userID, gameID uint64) (*models.UserGamePlay, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	val, err := r.client.Get(context.Background(), fmt.Sprintf("game:ugp:%d:%d", userID, gameID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var ugp models.UserGamePlay
	if err := json.Unmarshal([]byte(val), &ugp); err != nil {
		return nil, false, err
	}
	return &ugp, true, nil
}

func (r *RedisGameRepository) SetUserGamePlay(ugp *models.UserGamePlay) error {
	if r == nil || r.client == nil || ugp == nil {
		return nil
	}
	body, err := json.Marshal(ugp)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), fmt.Sprintf("game:ugp:%d:%d", ugp.UserID, ugp.GameID), body, gameCacheTTL).Err()
}

func (r *RedisGameRepository) DeleteUserGamePlay(userID, gameID uint64) error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Del(context.Background(), fmt.Sprintf("game:ugp:%d:%d", userID, gameID)).Err()
}

func (r *RedisGameRepository) GetUserGamePlayList(userID uint64) ([]*models.UserGamePlay, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	val, err := r.client.Get(context.Background(), fmt.Sprintf("game:ugp:list:%d", userID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var list []*models.UserGamePlay
	if err := json.Unmarshal([]byte(val), &list); err != nil {
		return nil, false, err
	}
	return list, true, nil
}

func (r *RedisGameRepository) SetUserGamePlayList(userID uint64, list []*models.UserGamePlay) error {
	if r == nil || r.client == nil {
		return nil
	}
	body, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), fmt.Sprintf("game:ugp:list:%d", userID), body, gameCacheTTL).Err()
}

func (r *RedisGameRepository) DeleteUserGamePlayList(userID uint64) error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Del(context.Background(), fmt.Sprintf("game:ugp:list:%d", userID)).Err()
}

// --- GameTopicMap 缓存 ---

func (r *RedisGameRepository) GetGameTopicMap(gameID uint64) (uint, bool, error) {
	if r == nil || r.client == nil {
		return 0, false, nil
	}
	val, err := r.client.Get(context.Background(), fmt.Sprintf("game:topic:map:%d", gameID)).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, false, nil
		}
		return 0, false, err
	}
	var topicID uint
	if _, e := fmt.Sscanf(val, "%d", &topicID); e != nil {
		return 0, false, e
	}
	return topicID, true, nil
}

func (r *RedisGameRepository) SetGameTopicMap(gameID uint64, topicID uint) error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Set(context.Background(), fmt.Sprintf("game:topic:map:%d", gameID),
		fmt.Sprintf("%d", topicID), gameCacheTTL).Err()
}

func (r *RedisGameRepository) DeleteGameTopicMap(gameID uint64) error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Del(context.Background(), fmt.Sprintf("game:topic:map:%d", gameID)).Err()
}

// --- 游戏列表缓存 game:list:p:{page}:s:{size} ---

type gameListCachePayload struct {
	List  []*models.Game `json:"list"`
	Total int64          `json:"total"`
}

func (r *RedisGameRepository) GetGamesList(page, size int) ([]*models.Game, int64, bool, error) {
	if r == nil || r.client == nil {
		return nil, 0, false, nil
	}
	key := fmt.Sprintf("game:list:p:%d:s:%d", page, size)
	val, err := r.client.Get(context.Background(), key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, 0, false, nil
		}
		return nil, 0, false, err
	}
	var p gameListCachePayload
	if err := json.Unmarshal([]byte(val), &p); err != nil {
		return nil, 0, false, err
	}
	return p.List, p.Total, true, nil
}

func (r *RedisGameRepository) SetGamesList(page, size int, list []*models.Game, total int64) error {
	if r == nil || r.client == nil {
		return nil
	}
	key := fmt.Sprintf("game:list:p:%d:s:%d", page, size)
	p := gameListCachePayload{List: list, Total: total}
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), key, body, gameCacheTTL).Err()
}

// InvalidateGamesListCache 游戏增删改后清除全部列表页缓存
func (r *RedisGameRepository) InvalidateGamesListCache() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.DeleteKeysByPattern("game:list:*")
}
