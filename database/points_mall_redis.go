package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisPointsMallRepository 商城库存（Redis 原子扣减，与 MySQL 码库对齐）。
type RedisPointsMallRepository struct {
	client *redis.Client
}

func NewRedisPointsMallRepository(client *redis.Client) *RedisPointsMallRepository {
	if client == nil {
		return nil
	}
	return &RedisPointsMallRepository{client: client}
}

func mallStockKey(productID uint64) string {
	return fmt.Sprintf("mall:stock:%d", productID)
}

func mallRedeemLockKey(userID uint64) string {
	return fmt.Sprintf("mall:redeem:lock:%d", userID)
}

func mallIdempotencyKey(userID uint64, idem string) string {
	return fmt.Sprintf("mall:idem:%d:%s", userID, idem)
}

// redeemStockScript 原子扣减库存：成功返回 1，库存不足返回 0。
var redeemStockScript = redis.NewScript(`
local key = KEYS[1]
local cur = tonumber(redis.call('GET', key) or '0')
if cur <= 0 then
  return 0
end
redis.call('DECR', key)
return 1
`)

// SetStock 同步可售库存到 Redis。
func (r *RedisPointsMallRepository) SetStock(productID uint64, stock int64) error {
	if r == nil || r.client == nil {
		return nil
	}
	ctx := context.Background()
	return r.client.Set(ctx, mallStockKey(productID), stock, 0).Err()
}

// GetStock 读取 Redis 库存（未初始化时返回 -1）。
func (r *RedisPointsMallRepository) GetStock(productID uint64) (int64, error) {
	if r == nil || r.client == nil {
		return 0, nil
	}
	ctx := context.Background()
	n, err := r.client.Get(ctx, mallStockKey(productID)).Int64()
	if err == redis.Nil {
		return -1, nil
	}
	return n, err
}

// TryDecrStock Lua 原子扣减，防止超卖。
func (r *RedisPointsMallRepository) TryDecrStock(productID uint64) (bool, error) {
	if r == nil || r.client == nil {
		return true, nil
	}
	ctx := context.Background()
	v, err := redeemStockScript.Run(ctx, r.client, []string{mallStockKey(productID)}).Int64()
	if err != nil {
		return false, err
	}
	return v == 1, nil
}

// IncrStock 回滚库存（MySQL 落库失败时）。
func (r *RedisPointsMallRepository) IncrStock(productID uint64) error {
	if r == nil || r.client == nil {
		return nil
	}
	ctx := context.Background()
	return r.client.Incr(ctx, mallStockKey(productID)).Err()
}

// AcquireRedeemLock 同一用户串行兑换，避免并发双扣。
func (r *RedisPointsMallRepository) AcquireRedeemLock(userID uint64, ttl time.Duration) (bool, error) {
	if r == nil || r.client == nil {
		return true, nil
	}
	if ttl <= 0 {
		ttl = 15 * time.Second
	}
	ctx := context.Background()
	return r.client.SetNX(ctx, mallRedeemLockKey(userID), "1", ttl).Result()
}

func (r *RedisPointsMallRepository) ReleaseRedeemLock(userID uint64) {
	if r == nil || r.client == nil {
		return
	}
	ctx := context.Background()
	_ = r.client.Del(ctx, mallRedeemLockKey(userID)).Err()
}

// GetIdempotentOrderID 幂等键命中则返回已有订单 ID。
func (r *RedisPointsMallRepository) GetIdempotentOrderID(userID uint64, idem string) (uint64, bool, error) {
	if r == nil || r.client == nil || idem == "" {
		return 0, false, nil
	}
	ctx := context.Background()
	val, err := r.client.Get(ctx, mallIdempotencyKey(userID, idem)).Uint64()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return val, true, nil
}

// SaveIdempotentOrderID 记录幂等键 → 订单（24h）。
func (r *RedisPointsMallRepository) SaveIdempotentOrderID(userID uint64, idem string, orderID uint64) error {
	if r == nil || r.client == nil || idem == "" || orderID == 0 {
		return nil
	}
	ctx := context.Background()
	return r.client.Set(ctx, mallIdempotencyKey(userID, idem), orderID, 24*time.Hour).Err()
}
