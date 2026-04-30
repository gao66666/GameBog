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
type RedisArticleRepository struct {
	client *redis.Client
}
type RedisUserRepository struct {
	client *redis.Client
}
type RedisCommentRepository struct {
	client *redis.Client
}
type RedisFollowRepository struct {
	client *redis.Client
}
type RedisGameRepository struct {
	client *redis.Client
}

func NewRedisArticleRepository(client *redis.Client) *RedisArticleRepository {
	return &RedisArticleRepository{client: client}
}

func NewRedisUserRepository(client *redis.Client) *RedisUserRepository {
	return &RedisUserRepository{client: client}
}

func NewRedisCommentRepository(client *redis.Client) *RedisCommentRepository {
	return &RedisCommentRepository{client: client}
}

func NewRedisFollowRepository(client *redis.Client) *RedisFollowRepository {
	return &RedisFollowRepository{client: client}
}

func NewRedisGameRepository(client *redis.Client) *RedisGameRepository {
	return &RedisGameRepository{client: client}
}

// Init 初始化Redis连接
func RedisInit(cfg *setting.RedisConfig) (*redis.Client, error) {
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
		return nil, err
	}

	return client, err
}

// Close 关闭连接
func (r *RedisArticleRepository) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}
