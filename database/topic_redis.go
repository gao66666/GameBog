package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/redis/go-redis/v9"
)

const topicListCacheTTL = 5 * time.Minute

type RedisTopicRepository struct {
	client *redis.Client
}

func NewRedisTopicRepository(client *redis.Client) *RedisTopicRepository {
	return &RedisTopicRepository{client: client}
}

type topicListCachePayload struct {
	List  []*models.Topic `json:"list"`
	Total int64           `json:"total"`
}

func topicListCacheKey(page, size int) string {
	return fmt.Sprintf("topic:active:list:p:%d:s:%d", page, size)
}

func (r *RedisTopicRepository) GetActiveTopicsPaged(page, size int) ([]*models.Topic, int64, bool, error) {
	if r == nil || r.client == nil {
		return nil, 0, false, nil
	}
	val, err := r.client.Get(context.Background(), topicListCacheKey(page, size)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, 0, false, nil
		}
		return nil, 0, false, err
	}
	var payload topicListCachePayload
	if err := json.Unmarshal([]byte(val), &payload); err != nil {
		return nil, 0, false, err
	}
	return payload.List, payload.Total, true, nil
}

func (r *RedisTopicRepository) SetActiveTopicsPaged(page, size int, list []*models.Topic, total int64) error {
	if r == nil || r.client == nil {
		return nil
	}
	payload := topicListCachePayload{List: list, Total: total}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), topicListCacheKey(page, size), body, topicListCacheTTL).Err()
}

func (r *RedisTopicRepository) InvalidateActiveTopicsListCache() error {
	if r == nil || r.client == nil {
		return nil
	}
	return deleteKeysByPattern(r.client, "topic:active:list:*")
}
