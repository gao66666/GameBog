package database

import (
	"context"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/setting"
	"github.com/redis/go-redis/v9"
)

var (
	client *redis.Client
	ctx    = context.Background()
)

// RedisRepository Redis操作封装
type RedisRepository struct {
	client *redis.Client
}

// NewRedisRepository 创建RedisRepository实例
func NewRedisRepository(client *redis.Client) *RedisRepository {
	return &RedisRepository{client: client}
}

// Init 初始化Redis连接
func RedisInit(cfg *setting.RedisConfig) (*RedisRepository, error) {
	client = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
		// 使用默认超时设置
	})
	// 设置超时上下文测试连接
	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := client.Ping(testCtx).Result()
	if err != nil {
		return nil, fmt.Errorf("redis连接失败: %v", err)
	}

	return NewRedisRepository(client), nil
}

// Close 关闭连接
func (r *RedisRepository) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// 封装常用操作
func (r *RedisRepository) Get(key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *RedisRepository) Set(key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *RedisRepository) Del(key string) error {
	return r.client.Del(ctx, key).Err()
}
