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
	ctx := context.Background()
	var cursor uint64
	for {
		keys, next, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.client.Del(ctx, keys...).Err(); err != nil {
				return err
			}
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
