package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gao66666/GoBlog/models"
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

// --- RedisCommentRepository 方法 ---

// IncrStats 增加评论的点赞计数
func (r *RedisCommentRepository) IncrStats(cid uint64) error {
	ctx := context.Background()
	key := fmt.Sprintf("comment:stats:%d", cid)
	return r.client.HIncrBy(ctx, key, "like", 1).Err()
}

// DecrStats 减少评论的点赞计数
func (r *RedisCommentRepository) DecrStats(cid uint64) error {
	ctx := context.Background()
	key := fmt.Sprintf("comment:stats:%d", cid)
	return r.client.HIncrBy(ctx, key, "like", -1).Err()
}

// InitStats 初始化评论的点赞计数
func (r *RedisCommentRepository) InitStats(cid uint64, likes int64) {
	ctx := context.Background()
	key := fmt.Sprintf("comment:stats:%d", cid)
	r.client.HSetNX(ctx, key, "like", likes)
	r.client.Expire(ctx, key, 86400*time.Second)
}

// GetStats 获取评论的点赞数
func (r *RedisCommentRepository) GetStats(cid uint64) (int64, error) {
	ctx := context.Background()
	key := fmt.Sprintf("comment:stats:%d", cid)
	res, err := r.client.HGet(ctx, key, "like").Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(res, 10, 64)
}

// --- CY 列表缓存 ---

const cyListCacheTTL = 10 * time.Minute

func (r *RedisCommentRepository) GetUserCYList(userID uint64) ([]*models.Comment, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	val, err := r.client.Get(ctx, fmt.Sprintf("comment:cy:list:%d", userID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var list []*models.Comment
	if err := json.Unmarshal([]byte(val), &list); err != nil {
		return nil, false, err
	}
	return list, true, nil
}

func (r *RedisCommentRepository) SetUserCYList(userID uint64, list []*models.Comment) error {
	if r == nil || r.client == nil {
		return nil
	}
	body, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, fmt.Sprintf("comment:cy:list:%d", userID), body, cyListCacheTTL).Err()
}

func (r *RedisCommentRepository) DeleteUserCYList(userID uint64) error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Del(ctx, fmt.Sprintf("comment:cy:list:%d", userID)).Err()
}
