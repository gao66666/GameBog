package database

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const channelKeyPrefix = "goblog:channel:"

// 合成 user_id 起点，避免与真实用户雪花 id 重叠。
const channelSyntheticUIDBase uint64 = 9_000_000_000_000_000_000

// ChannelStore IM 通道用户与会话的 Redis 映射（平台无关）。
type ChannelStore struct {
	client *redis.Client
}

func NewChannelStore(c *redis.Client) *ChannelStore {
	if c == nil {
		return nil
	}
	return &ChannelStore{client: c}
}

func channelIdentityKey(channel, tenantID, externalUser string) string {
	return channelKeyPrefix + "id:" + channel + ":" + tenantID + ":" + externalUser
}

func channelSessionKey(channel, tenantID, chatKey string) string {
	return channelKeyPrefix + "sess:" + channel + ":" + tenantID + ":" + chatKey
}

func channelDedupKey(channel, eventID string) string {
	return channelKeyPrefix + "dedup:" + channel + ":" + eventID
}

// GetOrCreateSyntheticUserID 为 IM 用户分配稳定的合成 user_id（>0）。
func (s *ChannelStore) GetOrCreateSyntheticUserID(ctx context.Context, channel, tenantID, externalUser string) (uint64, error) {
	if s == nil || s.client == nil {
		return 0, fmt.Errorf("channel store unavailable")
	}
	channel = strings.TrimSpace(channel)
	tenantID = strings.TrimSpace(tenantID)
	externalUser = strings.TrimSpace(externalUser)
	if channel == "" || tenantID == "" || externalUser == "" {
		return 0, fmt.Errorf("invalid channel identity")
	}
	key := channelIdentityKey(channel, tenantID, externalUser)
	if v, err := s.client.Get(ctx, key).Result(); err == nil && v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err == nil && n > 0 {
			return n, nil
		}
	}
	seq, err := s.client.Incr(ctx, channelKeyPrefix+"uid_seq").Result()
	if err != nil {
		return 0, err
	}
	uid := channelSyntheticUIDBase + uint64(seq)
	ttl := 365 * 24 * time.Hour
	if err := s.client.Set(ctx, key, strconv.FormatUint(uid, 10), ttl).Err(); err != nil {
		return 0, err
	}
	return uid, nil
}

// GetOrCreateChatSessionID 按 chat_key 绑定 Agent 会话（与 Web 侧边栏隔离）。
func (s *ChannelStore) GetOrCreateChatSessionID(ctx context.Context, chat *RedisAgentChatStore, channel, tenantID string, userID uint64, chatKey string) (string, error) {
	if s == nil || s.client == nil || chat == nil || userID == 0 {
		return "", fmt.Errorf("invalid channel session args")
	}
	channel = strings.TrimSpace(channel)
	tenantID = strings.TrimSpace(tenantID)
	chatKey = strings.TrimSpace(chatKey)
	if channel == "" || tenantID == "" || chatKey == "" {
		return "", fmt.Errorf("invalid channel chat key")
	}
	key := channelSessionKey(channel, tenantID, chatKey)
	if sid, err := s.client.Get(ctx, key).Result(); err == nil {
		sid = strings.TrimSpace(sid)
		if sid != "" && chat.SessionExists(ctx, userID, sid) {
			return sid, nil
		}
	}
	sid, err := chat.CreateSession(ctx, userID)
	if err != nil || sid == "" {
		return "", err
	}
	ttl := 90 * 24 * time.Hour
	_ = s.client.Set(ctx, key, sid, ttl).Err()
	return sid, nil
}

// MarkEventProcessed 事件幂等；已处理过返回 false。
func (s *ChannelStore) MarkEventProcessed(ctx context.Context, channel, eventID string) (bool, error) {
	if s == nil || s.client == nil {
		return true, nil
	}
	channel = strings.TrimSpace(channel)
	eventID = strings.TrimSpace(eventID)
	if channel == "" || eventID == "" {
		return true, nil
	}
	ok, err := s.client.SetNX(ctx, channelDedupKey(channel, eventID), "1", 48*time.Hour).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// FeishuStore 兼容旧名，指向 ChannelStore。
type FeishuStore = ChannelStore

func NewFeishuStore(c *redis.Client) *FeishuStore {
	return NewChannelStore(c)
}
