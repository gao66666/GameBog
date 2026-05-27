package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/redis/go-redis/v9"
)

const (
	userBaseCacheTTL       = 2 * time.Hour
	userSocialStatsTTL     = 30 * time.Minute
	userSocialStatsKeyFmt  = "user:social_stats:%d"
	userAccountBalanceKeyFmt = "user:account_balance:%d"
)

type cachedUserSocialStats struct {
	FollowingUsers int64 `json:"following_users"` // 我关注的用户数（follows 表）
	Followers      int64 `json:"followers"`       // 粉丝数（users.following_count 列，语义为「被关注数」）
}

func userSocialStatsKey(userID uint64) string {
	return fmt.Sprintf(userSocialStatsKeyFmt, userID)
}

func (r *RedisUserRepository) GetUserSocialStats(userID uint64) (*cachedUserSocialStats, bool, error) {
	if r == nil || r.client == nil || userID == 0 {
		return nil, false, nil
	}
	ctx := context.Background()
	val, err := r.client.Get(ctx, userSocialStatsKey(userID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var out cachedUserSocialStats
	if err := json.Unmarshal([]byte(val), &out); err != nil {
		return nil, false, err
	}
	return &out, true, nil
}

func (r *RedisUserRepository) SetUserSocialStats(userID uint64, followingUsers, followers int64) error {
	if r == nil || r.client == nil || userID == 0 {
		return nil
	}
	body, err := json.Marshal(cachedUserSocialStats{
		FollowingUsers: followingUsers,
		Followers:      followers,
	})
	if err != nil {
		return err
	}
	ctx := context.Background()
	return r.client.Set(ctx, userSocialStatsKey(userID), body, userSocialStatsTTL).Err()
}

func (r *RedisUserRepository) DelUserSocialStats(userID uint64) error {
	if r == nil || r.client == nil || userID == 0 {
		return nil
	}
	ctx := context.Background()
	return r.client.Del(ctx, userSocialStatsKey(userID)).Err()
}

type cachedUserBase struct {
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
	Email    string `json:"email"`
	Avatar   string `json:"avatar"`
	Github   string `json:"github"`
}

func userBaseKey(userID uint64) string {
	return fmt.Sprintf("user:base:%d", userID)
}

func (r *RedisUserRepository) GetUserBase(userID uint64) (*cachedUserBase, bool, error) {
	if r == nil || r.client == nil {
		return nil, false, nil
	}
	if userID == 0 {
		return nil, false, nil
	}

	ctx := context.Background()
	val, err := r.client.Get(ctx, userBaseKey(userID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var out cachedUserBase
	if err := json.Unmarshal([]byte(val), &out); err != nil {
		return nil, false, err
	}
	return &out, true, nil
}

func (r *RedisUserRepository) SetUserBase(u *models.User) error {
	if r == nil || r.client == nil {
		return nil
	}
	if u == nil || u.ID == 0 {
		return nil
	}

	ctx := context.Background()
	body, err := json.Marshal(cachedUserBase{
		UserID:   fmt.Sprintf("%d", u.ID),
		UserName: u.Name,
		Email:    u.Email,
		Avatar:   u.Avatar,
		Github:   u.Github,
	})
	if err != nil {
		return err
	}

	return r.client.Set(ctx, userBaseKey(u.ID), body, userBaseCacheTTL).Err()
}

func (r *RedisUserRepository) DelUserBase(userID uint64) error {
	if r == nil || r.client == nil {
		return nil
	}
	if userID == 0 {
		return nil
	}
	ctx := context.Background()
	return r.client.Del(ctx, userBaseKey(userID)).Err()
}

func userAccountBalanceKey(userID uint64) string {
	return fmt.Sprintf(userAccountBalanceKeyFmt, userID)
}

// GetAccountBalance 读取账户余额 Redis 缓存（未命中 ok=false）。
func (r *RedisUserRepository) GetAccountBalance(userID uint64) (balance int64, ok bool, err error) {
	if r == nil || r.client == nil || userID == 0 {
		return 0, false, nil
	}
	ctx := context.Background()
	val, err := r.client.Get(ctx, userAccountBalanceKey(userID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, false, nil
		}
		return 0, false, err
	}
	var n int64
	if _, scanErr := fmt.Sscanf(val, "%d", &n); scanErr != nil {
		return 0, false, scanErr
	}
	return n, true, nil
}

// SetAccountBalance 写入账户余额 Redis 缓存。
func (r *RedisUserRepository) SetAccountBalance(userID uint64, balance int64) error {
	if r == nil || r.client == nil || userID == 0 {
		return nil
	}
	ctx := context.Background()
	return r.client.Set(ctx, userAccountBalanceKey(userID), fmt.Sprintf("%d", balance), userBaseCacheTTL).Err()
}

// DelAccountBalance 删除账户余额缓存（扣款等变动后失效，下次从 MySQL 重读）。
func (r *RedisUserRepository) DelAccountBalance(userID uint64) error {
	if r == nil || r.client == nil || userID == 0 {
		return nil
	}
	ctx := context.Background()
	return r.client.Del(ctx, userAccountBalanceKey(userID)).Err()
}
