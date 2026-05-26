package database

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const searchCacheTTL = 90 * time.Second

type RedisSearchRepository struct {
	client *redis.Client
}

func NewRedisSearchRepository(client *redis.Client) *RedisSearchRepository {
	return &RedisSearchRepository{client: client}
}

func NormalizeSearchQuery(q string) string {
	return strings.ToLower(strings.TrimSpace(q))
}

func searchCacheKey(normQ string, page, size int) string {
	sum := sha256.Sum256([]byte(normQ))
	return fmt.Sprintf("search:global:%x:p:%d:s:%d", sum[:8], page, size)
}

func (r *RedisSearchRepository) GetGlobalSearch(normQ string, page, size int, dest any) (bool, error) {
	if r == nil || r.client == nil || dest == nil {
		return false, nil
	}
	val, err := r.client.Get(context.Background(), searchCacheKey(normQ, page, size)).Result()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal([]byte(val), dest); err != nil {
		return false, err
	}
	return true, nil
}

func (r *RedisSearchRepository) SetGlobalSearch(normQ string, page, size int, payload any) error {
	if r == nil || r.client == nil || payload == nil {
		return nil
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), searchCacheKey(normQ, page, size), body, searchCacheTTL).Err()
}
