package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisPointsRepository 积分相关 Redis 操作（每日上限、签到）
type RedisPointsRepository struct {
	client *redis.Client
}

func NewRedisPointsRepository(client *redis.Client) *RedisPointsRepository {
	if client == nil {
		return nil
	}
	return &RedisPointsRepository{client: client}
}

// dailyLimitKey 每日上限 key
// 格式: points:daily:{userID}:{refType}:{YYYY-MM-DD}
func dailyLimitKey(userID uint64, refType, date string) string {
	return fmt.Sprintf("points:daily:%d:%s:%s", userID, refType, date)
}

// CheckAndIncrDailyLimit 检查并增加每日上限计数器
// 返回当前累计值（包含本次），若超限则自动回滚并返回 error
func (r *RedisPointsRepository) CheckAndIncrDailyLimit(userID uint64, refType string, limit int64) (int64, error) {
	if r == nil || r.client == nil {
		return 0, nil // Redis 不可用则降级，不拦截
	}
	ctx := context.Background()
	key := dailyLimitKey(userID, refType, time.Now().Format("2006-01-02"))

	// Lua 脚本：原子 INCR 并检查
	script := redis.NewScript(`
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local ttl = tonumber(ARGV[2])

		local cur = redis.call("INCR", key)
		if cur == 1 then
			redis.call("EXPIRE", key, ttl)
		end

		if cur > limit then
			redis.call("DECR", key)
			return -1
		end

		return cur
	`)

	result, err := script.Run(ctx, r.client, []string{key}, limit, 48*3600).Int64()
	if err != nil {
		return 0, err
	}
	if result == -1 {
		return limit, ErrDailyLimitExceeded
	}
	return result, nil
}

// GetDailyLimit 获取当前每日累计值（不增加）
func (r *RedisPointsRepository) GetDailyLimit(userID uint64, refType string) (int64, error) {
	if r == nil || r.client == nil {
		return 0, nil
	}
	ctx := context.Background()
	key := dailyLimitKey(userID, refType, time.Now().Format("2006-01-02"))

	val, err := r.client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return 0, nil
		}
		return 0, err
	}
	return val, nil
}

// RollbackDailyLimit 回滚每日上限计数器（业务失败时回滚）
func (r *RedisPointsRepository) RollbackDailyLimit(userID uint64, refType string) error {
	if r == nil || r.client == nil {
		return nil
	}
	ctx := context.Background()
	key := dailyLimitKey(userID, refType, time.Now().Format("2006-01-02"))

	return r.client.Decr(ctx, key).Err()
}

// --- 签到 Redis Bitmap ---

// checkinKey 签到 Bitmap key
// 格式: checkin:{year}:{userID}
func checkinKey(userID uint64, year string) string {
	return fmt.Sprintf("checkin:%s:%d", year, userID)
}

// MarkCheckin 记录签到（Bitmap）
func (r *RedisPointsRepository) MarkCheckin(userID uint64) (bool, error) {
	if r == nil || r.client == nil {
		return false, nil
	}
	ctx := context.Background()
	now := time.Now()
	key := checkinKey(userID, now.Format("2006"))
	dayOfYear := now.YearDay() - 1 // 0-based

	// 检查今天是否已签到
	old, err := r.client.GetBit(ctx, key, int64(dayOfYear)).Result()
	if err != nil {
		return false, err
	}
	if old == 1 {
		return false, nil // 已签到
	}

	// 标记签到
	if err := r.client.SetBit(ctx, key, int64(dayOfYear), 1).Err(); err != nil {
		return false, err
	}
	// 设置 TTL 防止 key 永久堆积（两年后自动过期）
	if err := r.client.Expire(ctx, key, 2*365*24*time.Hour).Err(); err != nil {
		return false, err
	}
	return true, nil
}

// HasCheckedIn 检查今天是否已签到
func (r *RedisPointsRepository) HasCheckedIn(userID uint64) (bool, error) {
	if r == nil || r.client == nil {
		return false, nil
	}
	ctx := context.Background()
	now := time.Now()
	key := checkinKey(userID, now.Format("2006"))
	dayOfYear := now.YearDay() - 1

	bit, err := r.client.GetBit(ctx, key, int64(dayOfYear)).Result()
	if err != nil {
		return false, err
	}
	return bit == 1, nil
}

// GetCheckinCount 获取年签到天数
func (r *RedisPointsRepository) GetCheckinCount(userID uint64, year string) (int64, error) {
	if r == nil || r.client == nil {
		return 0, nil
	}
	ctx := context.Background()
	key := checkinKey(userID, year)

	count, err := r.client.BitCount(ctx, key, nil).Result()
	if err != nil {
		return 0, err
	}
	return count, nil
}
