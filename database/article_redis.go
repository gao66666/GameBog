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

	// 首页“最新博客”（全部）：按时间倒序的 ZSET，仅保留最近 18 条（3 页 * 6 条）。
	latestArticleZSetKey   = "article:latest"
	latestArticleKeepCount = 18

	// 本周热榜：按本地日历日分桶，合并最近 7 天（今天 + 前 6 天）；单日桶过期后自动回收。
	weeklyLeaderboardBuckets = 7
	leaderboardDayBucketTTL  = 10 * 24 * time.Hour
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

// weeklyLeaderboardDaySuffixes 返回最近 n 个本地日历日的 YYYYMMDD（含今天，向前共 n 天）。
func weeklyLeaderboardDaySuffixes(now time.Time, n int) []string {
	loc := time.Local
	out := make([]string, n)
	t := now.In(loc)
	for i := 0; i < n; i++ {
		d := t.AddDate(0, 0, -i)
		out[i] = d.Format("20060102")
	}
	return out
}

func weeklyLeaderboardRedisKeys(actionType string, now time.Time) []string {
	suffixes := weeklyLeaderboardDaySuffixes(now, weeklyLeaderboardBuckets)
	keys := make([]string, len(suffixes))
	for i, suf := range suffixes {
		keys[i] = fmt.Sprintf("article:leaderboard:%s:%s", actionType, suf)
	}
	return keys
}

func (r *RedisArticleRepository) UpdateLeaderboard(aid uint64, actionType string) error {
	ctx := context.Background()
	suffix := time.Now().In(time.Local).Format("20060102")
	key := fmt.Sprintf("article:leaderboard:%s:%s", actionType, suffix)
	member := strconv.FormatUint(aid, 10)

	pipe := r.client.TxPipeline()
	pipe.ZIncrBy(ctx, key, 1, member)
	pipe.Expire(ctx, key, leaderboardDayBucketTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// UpdateLeaderboardDecr 减少排行榜中的分数
func (r *RedisArticleRepository) UpdateLeaderboardDecr(aid uint64, actionType string) error {
	ctx := context.Background()
	suffix := time.Now().In(time.Local).Format("20060102")
	key := fmt.Sprintf("article:leaderboard:%s:%s", actionType, suffix)
	member := strconv.FormatUint(aid, 10)

	pipe := r.client.TxPipeline()
	pipe.ZIncrBy(ctx, key, -1, member)
	pipe.Expire(ctx, key, leaderboardDayBucketTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// mergeWeeklyLeaderboard 将最近 7 天的日桶合并到 dest（分数 SUM），供查询或下游逻辑使用。
func (r *RedisArticleRepository) mergeWeeklyLeaderboard(ctx context.Context, actionType, dest string, now time.Time) error {
	keys := weeklyLeaderboardRedisKeys(actionType, now)
	if len(keys) == 0 {
		return nil
	}
	return r.client.ZUnionStore(ctx, dest, &redis.ZStore{
		Keys:      keys,
		Aggregate: "SUM",
	}).Err()
}

// GetTopArticleIDs 获取前 N 名的文章 ID（最近 7 天阅读/点赞合计）。
func (r *RedisArticleRepository) GetTopArticleIDs(actionType string, n int) ([]uint64, error) {
	ctx := context.Background()
	now := time.Now()
	tmpKey := fmt.Sprintf("article:leaderboard:merge:%s:%d", actionType, now.UnixNano())
	if err := r.mergeWeeklyLeaderboard(ctx, actionType, tmpKey, now); err != nil {
		return nil, err
	}
	defer func() { _ = r.client.Del(ctx, tmpKey).Err() }()

	result, err := r.client.ZRevRange(ctx, tmpKey, 0, int64(n-1)).Result()
	if err != nil {
		return nil, err
	}

	ids := make([]uint64, 0, len(result))
	for _, s := range result {
		id, _ := strconv.ParseUint(s, 10, 64)
		ids = append(ids, id)
	}
	return ids, nil
}

// 获取前10名
func (r *RedisArticleRepository) GetTopArticles(actionType string, n int64) ([]string, error) {
	ctx := context.Background()
	now := time.Now()
	tmpKey := fmt.Sprintf("article:leaderboard:merge:%s:%d", actionType, now.UnixNano())
	if err := r.mergeWeeklyLeaderboard(ctx, actionType, tmpKey, now); err != nil {
		return nil, err
	}
	defer func() { _ = r.client.Del(ctx, tmpKey).Err() }()

	return r.client.ZRevRange(ctx, tmpKey, 0, n-1).Result()
}
func (r *RedisArticleRepository) TrimLeaderboard(actionType string, keep int64) error {
	ctx := context.Background()
	suffix := time.Now().In(time.Local).Format("20060102")
	key := fmt.Sprintf("article:leaderboard:%s:%s", actionType, suffix)
	// 保留前 keep 名，删掉排名在 -(keep+1) 之前的（仅裁剪「当天」桶）
	return r.client.ZRemRangeByRank(ctx, key, 0, -(keep + 1)).Err()
}

// AddLatestArticle 新文章写入首页“最新博客”（全部）ZSET。
// score 使用创建时间（秒级），member 为文章 ID（字符串）。
func (r *RedisArticleRepository) AddLatestArticle(aid uint64, createdAt time.Time) error {
	if r == nil || r.client == nil {
		return nil
	}
	if aid == 0 {
		return nil
	}
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	ctx := context.Background()
	member := strconv.FormatUint(aid, 10)
	score := float64(createdAt.Unix())

	pipe := r.client.TxPipeline()
	pipe.ZAdd(ctx, latestArticleZSetKey, redis.Z{Score: score, Member: member})
	// 只保留最新 18 条（删除更旧的）
	pipe.ZRemRangeByRank(ctx, latestArticleZSetKey, 0, -(int64(latestArticleKeepCount)+1))
	_, err := pipe.Exec(ctx)
	return err
}

// GetLatestArticleIDs 获取首页“最新博客”（全部）指定页的文章 ID 列表，并返回 ZSET 总数（最多 18）。
func (r *RedisArticleRepository) GetLatestArticleIDs(page int, size int) ([]uint64, int64, error) {
	if r == nil || r.client == nil {
		return []uint64{}, 0, nil
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 6
	}

	ctx := context.Background()
	total, err := r.client.ZCard(ctx, latestArticleZSetKey).Result()
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []uint64{}, 0, nil
	}

	start := int64((page - 1) * size)
	stop := start + int64(size) - 1
	members, err := r.client.ZRevRange(ctx, latestArticleZSetKey, start, stop).Result()
	if err != nil {
		return nil, 0, err
	}

	ids := make([]uint64, 0, len(members))
	for _, s := range members {
		id, _ := strconv.ParseUint(s, 10, 64)
		if id > 0 {
			ids = append(ids, id)
		}
	}

	// total 可能 >18（理论不会，因为 AddLatestArticle 会 trim），这里再兜底裁剪。
	if total > int64(latestArticleKeepCount) {
		total = int64(latestArticleKeepCount)
	}
	return ids, total, nil
}

// SeedLatestArticles 当 ZSET 为空时，用 DB 的最新 18 条回填。
func (r *RedisArticleRepository) SeedLatestArticles(articles []*models.Article) error {
	if r == nil || r.client == nil {
		return nil
	}
	if len(articles) == 0 {
		return nil
	}

	zs := make([]redis.Z, 0, len(articles))
	for _, a := range articles {
		if a == nil || a.ID == 0 {
			continue
		}
		ct := a.CreatedAt
		if ct.IsZero() {
			ct = time.Now()
		}
		zs = append(zs, redis.Z{Score: float64(ct.Unix()), Member: strconv.FormatUint(a.ID, 10)})
	}
	if len(zs) == 0 {
		return nil
	}

	ctx := context.Background()
	pipe := r.client.TxPipeline()
	pipe.ZAdd(ctx, latestArticleZSetKey, zs...)
	pipe.ZRemRangeByRank(ctx, latestArticleZSetKey, 0, -(int64(latestArticleKeepCount)+1))
	_, err := pipe.Exec(ctx)
	return err
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
	now := time.Now()
	tmpKey := fmt.Sprintf("article:leaderboard:merge:%s:%d", actionType, now.UnixNano())
	if err := r.mergeWeeklyLeaderboard(ctx, actionType, tmpKey, now); err != nil {
		return err
	}
	defer func() { _ = r.client.Del(ctx, tmpKey).Err() }()

	items, err := r.client.ZRevRangeWithScores(ctx, tmpKey, 0, topN-1).Result()
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

// --- ArticleCollection 缓存 ---

func (r *RedisArticleRepository) GetUserCollectionList(userID uint64) ([]*models.ArticleCollection, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	val, err := r.client.Get(ctx, fmt.Sprintf("article:col:list:%d", userID)).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	var list []*models.ArticleCollection
	if err := json.Unmarshal([]byte(val), &list); err != nil {
		return nil, false, err
	}
	return list, true, nil
}

func (r *RedisArticleRepository) SetUserCollectionList(userID uint64, list []*models.ArticleCollection) error {
	if r == nil || r.client == nil {
		return nil
	}
	body, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, fmt.Sprintf("article:col:list:%d", userID), body, articleCacheTTL).Err()
}

func (r *RedisArticleRepository) DeleteUserCollectionList(userID uint64) error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Del(ctx, fmt.Sprintf("article:col:list:%d", userID)).Err()
}
