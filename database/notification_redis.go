package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisNotificationRepository struct {
	client *redis.Client
}

func NewRedisNotificationRepository(client *redis.Client) *RedisNotificationRepository {
	return &RedisNotificationRepository{client: client}
}

func notifyUnreadKey(userID uint64) string {
	return fmt.Sprintf("notify:has_unread:%d", userID)
}

func (r *RedisNotificationRepository) SetHasUnread(userID uint64) error {
	ctx := context.Background()
	return r.client.Set(ctx, notifyUnreadKey(userID), 1, 7*24*time.Hour).Err()
}

func (r *RedisNotificationRepository) HasUnread(userID uint64) (bool, error) {
	ctx := context.Background()
	count, err := r.client.Exists(ctx, notifyUnreadKey(userID)).Result()
	return count > 0, err
}

func (r *RedisNotificationRepository) ClearHasUnread(userID uint64) error {
	ctx := context.Background()
	return r.client.Del(ctx, notifyUnreadKey(userID)).Err()
}
