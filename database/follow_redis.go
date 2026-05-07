package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/redis/go-redis/v9"
)

const topicFollowCacheTTL = 10 * time.Minute

func (r *RedisFollowRepository) GetTopicFollowList(userID uint64) ([]*models.TopicFollow, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	val, err := r.client.Get(context.Background(), fmt.Sprintf("follow:topic:list:%d", userID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var list []*models.TopicFollow
	if err := json.Unmarshal([]byte(val), &list); err != nil {
		return nil, false, err
	}
	return list, true, nil
}

func (r *RedisFollowRepository) SetTopicFollowList(userID uint64, list []*models.TopicFollow) error {
	if r == nil || r.client == nil {
		return nil
	}
	body, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), fmt.Sprintf("follow:topic:list:%d", userID), body, topicFollowCacheTTL).Err()
}

func (r *RedisFollowRepository) DeleteTopicFollowList(userID uint64) error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Del(context.Background(), fmt.Sprintf("follow:topic:list:%d", userID)).Err()
}
