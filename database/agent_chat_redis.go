package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis 键前缀（博客侧短期对话，供页面历史/网关注入上下文）。
// 多会话：ZSET 索引 + 每会话 list/meta；当前会话指针 current。
// Agent 进程另有 agent:short:{uid}:{session_id}:*（按会话分桶）。
const agentChatKeyPrefix = "goblog:agent:chat:"

const agentSessionDefaultTitle = "新对话"

// AgentChatMessage 单条对话（user / assistant）。
type AgentChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AgentSessionMeta 侧边栏展示的会话摘要。
type AgentSessionMeta struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// RedisAgentChatStore 在博客 Redis 中保存 AI 助手短期对话轮次（按会话）。
type RedisAgentChatStore struct {
	client *redis.Client
}

func NewRedisAgentChatStore(c *redis.Client) *RedisAgentChatStore {
	if c == nil {
		return nil
	}
	return &RedisAgentChatStore{client: c}
}

// --- 旧版单列表键（迁移用） ---

func (s *RedisAgentChatStore) legacyListKey(userID uint64) string {
	return fmt.Sprintf("%sshort:%d:list", agentChatKeyPrefix, userID)
}

func (s *RedisAgentChatStore) legacyStateKey(userID uint64) string {
	return fmt.Sprintf("%sshort:%d:state", agentChatKeyPrefix, userID)
}

func (s *RedisAgentChatStore) sessionsIndexKey(userID uint64) string {
	return fmt.Sprintf("%ssessions:%d", agentChatKeyPrefix, userID)
}

func (s *RedisAgentChatStore) sessionListKey(userID uint64, sessionID string) string {
	return fmt.Sprintf("%ss:%d:%s:list", agentChatKeyPrefix, userID, sessionID)
}

func (s *RedisAgentChatStore) sessionMetaKey(userID uint64, sessionID string) string {
	return fmt.Sprintf("%ss:%d:%s:meta", agentChatKeyPrefix, userID, sessionID)
}

func (s *RedisAgentChatStore) currentSessionKey(userID uint64) string {
	return fmt.Sprintf("%scurrent:%d", agentChatKeyPrefix, userID)
}

func newSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func truncateTitle(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return agentSessionDefaultTitle
	}
	r := []rune(s)
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

func firstUserSnippetFromListJSON(items []string) string {
	for _, item := range items {
		var m AgentChatMessage
		if json.Unmarshal([]byte(item), &m) != nil {
			continue
		}
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			return truncateTitle(m.Content, 28)
		}
	}
	return agentSessionDefaultTitle
}

// migrateLegacyIfNeeded 将旧版 goblog:agent:chat:short:{uid}:list 迁入会话 legacy。
func (s *RedisAgentChatStore) migrateLegacyIfNeeded(ctx context.Context, userID uint64) error {
	if s == nil || s.client == nil || userID == 0 {
		return nil
	}
	legacy := s.legacyListKey(userID)
	n, err := s.client.Exists(ctx, legacy).Result()
	if err != nil || n == 0 {
		return err
	}
	idxCount, err := s.client.ZCard(ctx, s.sessionsIndexKey(userID)).Result()
	if err != nil || idxCount > 0 {
		return err
	}
	items, err := s.client.LRange(ctx, legacy, 0, -1).Result()
	if err != nil {
		return err
	}
	sid := "legacy"
	now := time.Now().UTC()
	title := firstUserSnippetFromListJSON(items)
	meta := AgentSessionMeta{
		SessionID: sid,
		Title:     title,
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
	}
	mb, _ := json.Marshal(meta)
	newList := s.sessionListKey(userID, sid)
	ttl := 72 * time.Hour

	pipe := s.client.Pipeline()
	for _, it := range items {
		pipe.RPush(ctx, newList, it)
	}
	pipe.Expire(ctx, newList, ttl)
	pipe.Set(ctx, s.sessionMetaKey(userID, sid), string(mb), ttl)
	pipe.ZAdd(ctx, s.sessionsIndexKey(userID), redis.Z{Score: float64(now.UnixMilli()), Member: sid})
	pipe.Set(ctx, s.currentSessionKey(userID), sid, ttl)
	pipe.Del(ctx, legacy)
	pipe.Del(ctx, s.legacyStateKey(userID))
	_, err = pipe.Exec(ctx)
	return err
}

// ListSessions 返回最近更新的会话（含迁移后的 legacy）。
func (s *RedisAgentChatStore) ListSessions(ctx context.Context, userID uint64, limit int) ([]AgentSessionMeta, error) {
	if s == nil || s.client == nil || userID == 0 {
		return nil, nil
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)

	raw, err := s.client.ZRevRange(ctx, s.sessionsIndexKey(userID), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]AgentSessionMeta, 0, len(raw))
	for _, sid := range raw {
		meta, err := s.getSessionMeta(ctx, userID, sid)
		if err != nil || meta.SessionID == "" {
			meta = AgentSessionMeta{SessionID: sid, Title: agentSessionDefaultTitle}
		}
		out = append(out, meta)
	}
	return out, nil
}

func (s *RedisAgentChatStore) getSessionMeta(ctx context.Context, userID uint64, sessionID string) (AgentSessionMeta, error) {
	var z AgentSessionMeta
	raw, err := s.client.Get(ctx, s.sessionMetaKey(userID, sessionID)).Result()
	if err != nil || raw == "" {
		return z, err
	}
	_ = json.Unmarshal([]byte(raw), &z)
	return z, nil
}

// SessionExists 会话是否在索引中。
func (s *RedisAgentChatStore) SessionExists(ctx context.Context, userID uint64, sessionID string) bool {
	if s == nil || s.client == nil || userID == 0 || sessionID == "" {
		return false
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	_, err := s.client.ZScore(ctx, s.sessionsIndexKey(userID), sessionID).Result()
	return err == nil
}

// GetCurrentSessionID 当前选中的会话。
func (s *RedisAgentChatStore) GetCurrentSessionID(ctx context.Context, userID uint64) (string, error) {
	if s == nil || s.client == nil || userID == 0 {
		return "", nil
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	val, err := s.client.Get(ctx, s.currentSessionKey(userID)).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// SetCurrentSession 切换当前会话（须已存在于索引）。
func (s *RedisAgentChatStore) SetCurrentSession(ctx context.Context, userID uint64, sessionID string) error {
	if s == nil || s.client == nil || userID == 0 || sessionID == "" {
		return nil
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	_, err := s.client.ZScore(ctx, s.sessionsIndexKey(userID), sessionID).Result()
	if err != nil {
		return fmt.Errorf("session not found")
	}
	ttl := 72 * time.Hour
	return s.client.Set(ctx, s.currentSessionKey(userID), sessionID, ttl).Err()
}

// CreateSession 新建空会话并设为当前。
func (s *RedisAgentChatStore) CreateSession(ctx context.Context, userID uint64) (string, error) {
	if s == nil || s.client == nil || userID == 0 {
		return "", fmt.Errorf("invalid store")
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	sid := newSessionID()
	now := time.Now().UTC()
	meta := AgentSessionMeta{
		SessionID: sid,
		Title:     agentSessionDefaultTitle,
		CreatedAt: now.Format(time.RFC3339),
		UpdatedAt: now.Format(time.RFC3339),
	}
	mb, _ := json.Marshal(meta)
	ttl := 72 * time.Hour
	pipe := s.client.Pipeline()
	pipe.Set(ctx, s.sessionMetaKey(userID, sid), string(mb), ttl)
	pipe.ZAdd(ctx, s.sessionsIndexKey(userID), redis.Z{Score: float64(now.UnixMilli()), Member: sid})
	pipe.Expire(ctx, s.sessionsIndexKey(userID), ttl)
	pipe.Set(ctx, s.currentSessionKey(userID), sid, ttl)
	_, err := pipe.Exec(ctx)
	return sid, err
}

// ListMessages 返回某会话列表尾部最多 limit 条。
func (s *RedisAgentChatStore) ListMessages(ctx context.Context, userID uint64, sessionID string, limit int) ([]AgentChatMessage, error) {
	if s == nil || s.client == nil || userID == 0 || sessionID == "" || limit <= 0 {
		return nil, nil
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	raw, err := s.client.LRange(ctx, s.sessionListKey(userID, sessionID), int64(-limit), -1).Result()
	if err != nil {
		return nil, err
	}
	out := make([]AgentChatMessage, 0, len(raw))
	for _, item := range raw {
		var m AgentChatMessage
		if err := json.Unmarshal([]byte(item), &m); err != nil {
			continue
		}
		if m.Role == "" {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// AppendMessages 追加消息；更新 meta、ZSET 排序分。
func (s *RedisAgentChatStore) AppendMessages(ctx context.Context, userID uint64, sessionID string, msgs []AgentChatMessage, ttl time.Duration, maxKeep int) error {
	if s == nil || s.client == nil || userID == 0 || sessionID == "" || len(msgs) == 0 {
		return nil
	}
	if maxKeep < 4 {
		maxKeep = 12
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	key := s.sessionListKey(userID, sessionID)
	metaKey := s.sessionMetaKey(userID, sessionID)
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	meta, _ := s.getSessionMeta(ctx, userID, sessionID)
	if meta.SessionID == "" {
		meta.SessionID = sessionID
		meta.Title = agentSessionDefaultTitle
		meta.CreatedAt = nowStr
	}
	meta.UpdatedAt = nowStr
	for _, m := range msgs {
		if m.Role == "user" && (meta.Title == agentSessionDefaultTitle || meta.Title == "") {
			meta.Title = truncateTitle(m.Content, 28)
			break
		}
	}
	mb, _ := json.Marshal(meta)

	pipe := s.client.Pipeline()
	for _, m := range msgs {
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		pipe.RPush(ctx, key, string(b))
	}
	pipe.LTrim(ctx, key, int64(-maxKeep), -1)
	if ttl > 0 {
		pipe.Expire(ctx, key, ttl)
	}
	pipe.Set(ctx, metaKey, string(mb), ttl)
	pipe.ZAdd(ctx, s.sessionsIndexKey(userID), redis.Z{Score: float64(now.UnixMilli()), Member: sessionID})
	if ttl > 0 {
		pipe.Expire(ctx, s.sessionsIndexKey(userID), ttl)
	}
	pipe.Set(ctx, s.currentSessionKey(userID), sessionID, ttl)
	_, err := pipe.Exec(ctx)
	return err
}

// DeleteSession 删除会话；若删的是当前会话则切换到最近一条或清空指针。
func (s *RedisAgentChatStore) DeleteSession(ctx context.Context, userID uint64, sessionID string) (newCurrent string, err error) {
	if s == nil || s.client == nil || userID == 0 || sessionID == "" {
		return "", nil
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	idx := s.sessionsIndexKey(userID)
	cur, _ := s.client.Get(ctx, s.currentSessionKey(userID)).Result()

	pipe := s.client.Pipeline()
	pipe.Del(ctx, s.sessionListKey(userID, sessionID))
	pipe.Del(ctx, s.sessionMetaKey(userID, sessionID))
	pipe.ZRem(ctx, idx, sessionID)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return "", err
	}

	if cur != sessionID {
		return cur, nil
	}
	// 重选当前：索引中最新一条
	next, err := s.client.ZRevRange(ctx, idx, 0, 0).Result()
	if err != nil || len(next) == 0 {
		_ = s.client.Del(ctx, s.currentSessionKey(userID)).Err()
		return "", nil
	}
	newCurrent = next[0]
	ttl := 72 * time.Hour
	_ = s.client.Set(ctx, s.currentSessionKey(userID), newCurrent, ttl).Err()
	return newCurrent, nil
}

// EnsureCurrentOrCreate 无会话时创建一个并返回 id。
func (s *RedisAgentChatStore) EnsureCurrentOrCreate(ctx context.Context, userID uint64) (string, error) {
	if s == nil || s.client == nil || userID == 0 {
		return "", fmt.Errorf("invalid store")
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	cur, err := s.GetCurrentSessionID(ctx, userID)
	if err != nil {
		return "", err
	}
	if cur != "" && s.SessionExists(ctx, userID, cur) {
		return cur, nil
	}
	n, err := s.client.ZCard(ctx, s.sessionsIndexKey(userID)).Result()
	if err != nil {
		return "", err
	}
	if n == 0 {
		return s.CreateSession(ctx, userID)
	}
	// 有索引但 current 丢了：取最新
	top, err := s.client.ZRevRange(ctx, s.sessionsIndexKey(userID), 0, 0).Result()
	if err != nil || len(top) == 0 {
		return s.CreateSession(ctx, userID)
	}
	sid := top[0]
	ttl := 72 * time.Hour
	_ = s.client.Set(ctx, s.currentSessionKey(userID), sid, ttl).Err()
	return sid, nil
}

// ParseSessionIDQuery 解析 ?session_id=（hex 新会话 或 legacy 等字母迁移 id）。
func ParseSessionIDQuery(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 4 || len(v) > 128 {
		return ""
	}
	for _, c := range v {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			continue
		}
		return ""
	}
	return v
}

// SessionIDFromPath 路径中的会话 id。
func SessionIDFromPath(id string) string {
	return ParseSessionIDQuery(id)
}

// ClearAllSessionsForUser 删除用户全部会话索引与数据（慎用）。
func (s *RedisAgentChatStore) ClearAllSessionsForUser(ctx context.Context, userID uint64) error {
	if s == nil || s.client == nil || userID == 0 {
		return nil
	}
	_ = s.migrateLegacyIfNeeded(ctx, userID)
	idx := s.sessionsIndexKey(userID)
	ids, err := s.client.ZRange(ctx, idx, 0, -1).Result()
	if err != nil {
		return err
	}
	pipe := s.client.Pipeline()
	for _, sid := range ids {
		pipe.Del(ctx, s.sessionListKey(userID, sid))
		pipe.Del(ctx, s.sessionMetaKey(userID, sid))
	}
	pipe.Del(ctx, idx)
	pipe.Del(ctx, s.currentSessionKey(userID))
	pipe.Del(ctx, s.legacyListKey(userID))
	pipe.Del(ctx, s.legacyStateKey(userID))
	_, err = pipe.Exec(ctx)
	return err
}
