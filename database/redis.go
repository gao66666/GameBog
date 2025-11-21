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

// Init 初始化Redis连接
func RedisInit(cfg *setting.RedisConfig) error {
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
		return fmt.Errorf("redis连接失败: %v", err)
	}

	return nil
}

// Close 关闭连接
func RedisClose() error {
	if client != nil {
		return client.Close()
	}
	return nil
}

// 封装常用操作
func Get(key string) (string, error) {
	return client.Get(ctx, key).Result()
}

func Set(key string, value interface{}, expiration time.Duration) error {
	return client.Set(ctx, key, value, expiration).Err()
}

func Del(key string) error {
	return client.Del(ctx, key).Err()
}

// 获取原始客户端（用于复杂操作）
func Client() *redis.Client {
	return client
}
