package database

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/redis/go-redis/v9"
)

const (
	articleCacheTTL      = 10 * time.Minute
	articleCacheJitter   = 5 * time.Minute
	articleLogicTTL      = 2 * time.Minute
	articleNullCacheTTL  = 1 * time.Minute
	articleNullJitter    = 30 * time.Second
	articleCacheNullMark = "__NULL__"
	hotArticleSetKey      = "article:hotkeys"
)

type logicalArticleCache struct {
	Data     *models.Article `json:"data"`
	ExpireAt int64           `json:"expire_at"`
}

// GetStats 获取文章的阅读量和点赞数
func (r *RedisArticleRepository) GetStats(aid uint64) (views, likes int64, err error) {
	ctx := context.Background()
	key := fmt.Sprintf("article:stats:%d", aid)

	// 使用 HMGet 一次性获取多个字段
	res, err := r.client.HMGet(ctx, key, "view", "like").Result()
	if err != nil {
		return 0, 0, err
	}

	// Redis 返回的是 []interface{}，需要处理空值和类型转换
	if res[0] != nil {
		views, _ = strconv.ParseInt(res[0].(string), 10, 64)
	}
	if res[1] != nil {
		likes, _ = strconv.ParseInt(res[1].(string), 10, 64)
	}

	return views, likes, nil
}

// InitStats 用于缓存预热：当 Redis 没数时，从 MySQL 读完写回 Redis
func (r *RedisArticleRepository) InitStats(aid uint64, views, likes int64) {
	ctx := context.Background()
	key := fmt.Sprintf("article:stats:%d", aid)

	// 如果不存在则设置（防止覆盖掉刚刚产生的新点赞）
	r.client.HSetNX(ctx, key, "view", views)
	r.client.HSetNX(ctx, key, "like", likes)
	// 设置过期时间，比如 24 小时，避免冷数据常驻内存
	r.client.Expire(ctx, key, 86400*time.Second)
}

// IncrStats 给消费者使用，实时增加 Redis 计数
func (r *RedisArticleRepository) IncrStats(aid uint64, field string) error {
	ctx := context.Background()
	key := fmt.Sprintf("article:stats:%d", aid)
	// field 传入 "view" 或 "like"
	return r.client.HIncrBy(ctx, key, field, 1).Err()
}

// DecrStats 减少 Redis Hash 中的计数值
func (r *RedisArticleRepository) DecrStats(aid uint64, field string) error {
	ctx := context.Background()
	key := fmt.Sprintf("article:stats:%d", aid)

	// HIncrBy 传负数即为减少
	return r.client.HIncrBy(ctx, key, field, -1).Err()
}

func (r *RedisArticleRepository) UpdateLeaderboard(aid uint64, actionType string) error {
	ctx := context.Background()
	// 根据类型存入不同的排行榜（阅读榜/点赞榜）
	key := fmt.Sprintf("article:leaderboard:%s", actionType)

	// ZIncrBy: 如果成员不存在则创建并设置分数为1，存在则累加1
	return r.client.ZIncrBy(ctx, key, 1, strconv.FormatUint(aid, 10)).Err()
}

// UpdateLeaderboardDecr 减少排行榜中的分数
func (r *RedisArticleRepository) UpdateLeaderboardDecr(aid uint64, actionType string) error {
	ctx := context.Background()
	key := fmt.Sprintf("article:leaderboard:%s", actionType)

	// ZIncrBy 传 -1，实现分数减少
	// 即使分数降为 0，该成员依然会留在排行榜中，直到被 TrimLeaderboard 剔除
	return r.client.ZIncrBy(ctx, key, -1, strconv.FormatUint(aid, 10)).Err()
}

// GetTopArticleIDs 获取前 N 名的文章 ID
func (r *RedisArticleRepository) GetTopArticleIDs(actionType string, n int) ([]uint64, error) {
	ctx := context.Background()
	key := fmt.Sprintf("article:leaderboard:%s", actionType)

	// ZRevRange: 从高到低取 ID
	result, err := r.client.ZRevRange(ctx, key, 0, int64(n-1)).Result()
	if err != nil {
		return nil, err
	}

	// 将 string 转换为 uint64
	ids := make([]uint64, 0, len(result))
	for _, s := range result {
		id, _ := strconv.ParseUint(s, 10, 64)
		ids = append(ids, id)
	}
	return ids, nil
}

// 获取前10名
func (r *RedisArticleRepository) GetTopArticles(actionType string, n int64) ([]string, error) {
	key := fmt.Sprintf("article:leaderboard:%s", actionType)
	// ZRevRange: 按分数从高到低取前 N 名
	return r.client.ZRevRange(context.Background(), key, 0, n-1).Result()
}
func (r *RedisArticleRepository) TrimLeaderboard(actionType string, keep int64) error {
	ctx := context.Background()
	key := fmt.Sprintf("article:leaderboard:%s", actionType)
	// 保留前 keep 名，删掉排名在 -(keep+1) 之前的
	return r.client.ZRemRangeByRank(ctx, key, 0, -(keep + 1)).Err()
}

func (r *RedisArticleRepository) GetCachedArticle(aid uint64) (*models.Article, bool, bool, error) {
	ctx := context.Background()
	key := fmt.Sprintf("article:detail:%d", aid)

	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, false, nil
		}
		return nil, false, false, err
	}

	if val == articleCacheNullMark {
		return nil, true, true, nil
	}

	var wrapped logicalArticleCache
	if err := json.Unmarshal([]byte(val), &wrapped); err == nil && wrapped.Data != nil {
		return wrapped.Data, true, false, nil
	}

	var article models.Article
	if err := json.Unmarshal([]byte(val), &article); err != nil {
		return nil, false, false, err
	}

	return &article, true, false, nil
}

func (r *RedisArticleRepository) GetCachedArticleWithLogicalExpire(aid uint64) (*models.Article, bool, bool, bool, error) {
	ctx := context.Background()
	key := fmt.Sprintf("article:detail:%d", aid)

	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, false, false, nil
		}
		return nil, false, false, false, err
	}

	if val == articleCacheNullMark {
		return nil, false, true, true, nil
	}

	var wrapped logicalArticleCache
	if err := json.Unmarshal([]byte(val), &wrapped); err == nil && wrapped.Data != nil {
		isExpired := time.Now().Unix() > wrapped.ExpireAt
		return wrapped.Data, isExpired, true, false, nil
	}

	var article models.Article
	if err := json.Unmarshal([]byte(val), &article); err != nil {
		return nil, false, false, false, err
	}

	return &article, false, true, false, nil
}

func (r *RedisArticleRepository) SetArticleCache(aid uint64, article *models.Article) error {
	ctx := context.Background()
	key := fmt.Sprintf("article:detail:%d", aid)
	body, err := json.Marshal(article)
	if err != nil {
		return err
	}
	ttl := randomTTL(articleCacheTTL, articleCacheJitter)
	return r.client.Set(ctx, key, body, ttl).Err()
}

func (r *RedisArticleRepository) SetHotArticleCache(aid uint64, article *models.Article) error {
	ctx := context.Background()
	key := fmt.Sprintf("article:detail:%d", aid)
	body, err := json.Marshal(logicalArticleCache{
		Data:     article,
		ExpireAt: time.Now().Add(articleLogicTTL).Unix(),
	})
	if err != nil {
		return err
	}
	ttl := randomTTL(articleCacheTTL, articleCacheJitter)
	return r.client.Set(ctx, key, body, ttl).Err()
}

func (r *RedisArticleRepository) SetArticleNullCache(aid uint64) error {
	ctx := context.Background()
	key := fmt.Sprintf("article:detail:%d", aid)
	ttl := randomTTL(articleNullCacheTTL, articleNullJitter)
	return r.client.Set(ctx, key, articleCacheNullMark, ttl).Err()
}

func (r *RedisArticleRepository) AcquireArticleRebuildLock(aid uint64, ttl time.Duration) (bool, string, error) {
	ctx := context.Background()
	key := fmt.Sprintf("article:detail:lock:%d", aid)
	token, err := newLockToken()
	if err != nil {
		return false, "", err
	}
	locked, err := r.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return false, "", err
	}
	if !locked {
		return false, "", nil
	}
	return true, token, nil
}

func (r *RedisArticleRepository) ReleaseArticleRebuildLock(aid uint64, token string) {
	if token == "" {
		return
	}
	ctx := context.Background()
	key := fmt.Sprintf("article:detail:lock:%d", aid)
	script := redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
`)
	_, _ = script.Run(ctx, r.client, []string{key}, token).Result()
}

func (r *RedisArticleRepository) MarkProcessedOnce(namespace, unique string, ttl time.Duration) (bool, error) {
	ctx := context.Background()
	key := fmt.Sprintf("dedupe:%s:%s", namespace, unique)
	return r.client.SetNX(ctx, key, "1", ttl).Result()
}

func (r *RedisArticleRepository) MarkOnceByKey(key string, ttl time.Duration) (bool, error) {
	ctx := context.Background()
	return r.client.SetNX(ctx, key, "1", ttl).Result()
}

func (r *RedisArticleRepository) DeleteKey(key string) error {
	ctx := context.Background()
	return r.client.Del(ctx, key).Err()
}

func (r *RedisArticleRepository) IsHotArticle(aid uint64) (bool, error) {
	ctx := context.Background()
	return r.client.SIsMember(ctx, hotArticleSetKey, strconv.FormatUint(aid, 10)).Result()
}

func (r *RedisArticleRepository) RefreshHotArticlesFromLeaderboard(actionType string, topN int64, minScore float64, ttl time.Duration) error {
	ctx := context.Background()
	leaderboardKey := fmt.Sprintf("article:leaderboard:%s", actionType)

	items, err := r.client.ZRevRangeWithScores(ctx, leaderboardKey, 0, topN-1).Result()
	if err != nil {
		return err
	}

	pipe := r.client.Pipeline()
	pipe.Del(ctx, hotArticleSetKey)
	for _, item := range items {
		if item.Score < minScore {
			continue
		}
		member, ok := item.Member.(string)
		if !ok || member == "" {
			continue
		}
		pipe.SAdd(ctx, hotArticleSetKey, member)
	}
	pipe.Expire(ctx, hotArticleSetKey, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func randomTTL(base, jitter time.Duration) time.Duration {
	if jitter <= 0 {
		return base
	}
	return base + time.Duration(rand.Int63n(int64(jitter)))
}

func newLockToken() (string, error) {
	b := make([]byte, 16)
	if _, err := cryptorand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
