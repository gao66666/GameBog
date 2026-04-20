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

const userBaseCacheTTL = 2 * time.Hour

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
