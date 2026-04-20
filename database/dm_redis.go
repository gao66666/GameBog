package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/redis/go-redis/v9"
)

type RedisDMRepository struct {
	client *redis.Client
}

func NewRedisDMRepository(client *redis.Client) *RedisDMRepository {
	return &RedisDMRepository{client: client}
}

func dmPeersKey(userID uint64) string {
	return fmt.Sprintf("dm:peers:%d", userID)
}

func dmConvKey(userID uint64, peerID uint64) string {
	return fmt.Sprintf("dm:conv:%d:%d", userID, peerID)
}

func dmUnreadKey(userID uint64) string {
	return fmt.Sprintf("dm:unread:%d", userID)
}

func (r *RedisDMRepository) GetPeers(userID uint64) ([]models.DMPeer, bool, error) {
	ctx := context.Background()
	b, err := r.client.Get(ctx, dmPeersKey(userID)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var out []models.DMPeer
	if err := json.Unmarshal(b, &out); err != nil {
		_ = r.client.Del(ctx, dmPeersKey(userID)).Err()
		return nil, false, nil
	}
	return out, true, nil
}

func (r *RedisDMRepository) SetPeers(userID uint64, peers []models.DMPeer, ttl time.Duration) error {
	ctx := context.Background()
	b, err := json.Marshal(peers)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return r.client.Set(ctx, dmPeersKey(userID), b, ttl).Err()
}

func (r *RedisDMRepository) GetConversation(userID uint64, peerID uint64) ([]models.DMMessageDTO, bool, error) {
	ctx := context.Background()
	b, err := r.client.Get(ctx, dmConvKey(userID, peerID)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var out []models.DMMessageDTO
	if err := json.Unmarshal(b, &out); err != nil {
		_ = r.client.Del(ctx, dmConvKey(userID, peerID)).Err()
		return nil, false, nil
	}
	return out, true, nil
}

func (r *RedisDMRepository) SetConversation(userID uint64, peerID uint64, msgs []models.DMMessageDTO, ttl time.Duration) error {
	ctx := context.Background()
	b, err := json.Marshal(msgs)
	if err != nil {
		return err
	}
	if ttl <= 0 {
		ttl = 1 * time.Minute
	}
	return r.client.Set(ctx, dmConvKey(userID, peerID), b, ttl).Err()
}

func (r *RedisDMRepository) InvalidateConversation(userID uint64, peerID uint64) error {
	ctx := context.Background()
	return r.client.Del(ctx, dmConvKey(userID, peerID)).Err()
}

func (r *RedisDMRepository) InvalidatePeers(userID uint64) error {
	ctx := context.Background()
	return r.client.Del(ctx, dmPeersKey(userID)).Err()
}

func (r *RedisDMRepository) IncrUnread(userID uint64, delta int64) (int64, error) {
	if userID == 0 {
		return 0, nil
	}
	if delta == 0 {
		delta = 1
	}
	ctx := context.Background()
	return r.client.IncrBy(ctx, dmUnreadKey(userID), delta).Result()
}

func (r *RedisDMRepository) GetUnread(userID uint64) (int64, error) {
	if userID == 0 {
		return 0, nil
	}
	ctx := context.Background()
	v, err := r.client.Get(ctx, dmUnreadKey(userID)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

func (r *RedisDMRepository) ClearUnread(userID uint64) error {
	if userID == 0 {
		return nil
	}
	ctx := context.Background()
	return r.client.Del(ctx, dmUnreadKey(userID)).Err()
}
