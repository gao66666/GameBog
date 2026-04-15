package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"go.uber.org/zap"
)

const (
	dmMessageTTL = 7 * 24 * time.Hour
)

type DMService struct {
	dmRepo    *database.DMRepository
	userRepo  *database.UserRepository
	redisRepo *database.RedisDMRepository
}

func NewDMService(dmRepo *database.DMRepository, userRepo *database.UserRepository, redisRepo *database.RedisDMRepository) *DMService {
	s := &DMService{dmRepo: dmRepo, userRepo: userRepo, redisRepo: redisRepo}
	s.startCleanup()
	return s
}

func (s *DMService) startCleanup() {
	if s == nil || s.dmRepo == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			if err := s.dmRepo.DeleteExpired(time.Now()); err != nil {
				zap.L().Warn("清理过期私信失败", zap.Error(err))
			}
			<-ticker.C
		}
	}()
}

func (s *DMService) ListPeers(userID uint64) ([]models.DMPeer, error) {
	if userID == 0 {
		return nil, fmt.Errorf("invalid user_id")
	}
	if s.redisRepo != nil {
		if peers, hit, err := s.redisRepo.GetPeers(userID); err == nil && hit {
			return peers, nil
		}
	}

	ids, err := s.dmRepo.ListPeers(userID, 50)
	if err != nil {
		return nil, err
	}

	out := make([]models.DMPeer, 0, len(ids))
	for _, id := range ids {
		u, err := s.userRepo.GetUserByID(id)
		if err != nil {
			continue
		}
		out = append(out, models.DMPeer{UserID: u.ID, UserName: u.Name})
	}

	if s.redisRepo != nil {
		_ = s.redisRepo.SetPeers(userID, out, 30*time.Second)
	}
	return out, nil
}

func (s *DMService) ListMessages(userID, peerID uint64) ([]models.DMMessageDTO, error) {
	if userID == 0 || peerID == 0 {
		return nil, fmt.Errorf("invalid user_id/peer_id")
	}

	if s.redisRepo != nil {
		if msgs, hit, err := s.redisRepo.GetConversation(userID, peerID); err == nil && hit {
			return msgs, nil
		}
	}

	list, err := s.dmRepo.ListMessagesBetween(userID, peerID, 50)
	if err != nil {
		return nil, err
	}

	// repo 是 sent_at DESC，这里反转成 ASC
	out := make([]models.DMMessageDTO, 0, len(list))
	for i := len(list) - 1; i >= 0; i-- {
		m := list[i]
		if m == nil {
			continue
		}
		out = append(out, models.DMMessageDTO{
			FromUserID: m.FromUserID,
			ToUserID:   m.ToUserID,
			SentAt:     m.SentAt.Format("2006-01-02 15:04:05"),
			Content:    m.Content,
		})
	}

	if s.redisRepo != nil {
		_ = s.redisRepo.SetConversation(userID, peerID, out, 1*time.Minute)
	}
	return out, nil
}

func (s *DMService) SendMessage(fromUserID uint64, toUserID uint64, content string) (*models.DirectMessage, error) {
	if fromUserID == 0 || toUserID == 0 {
		return nil, fmt.Errorf("invalid from/to")
	}
	if fromUserID == toUserID {
		return nil, fmt.Errorf("cannot dm self")
	}
	c := strings.TrimSpace(content)
	if c == "" {
		return nil, fmt.Errorf("empty content")
	}
	if len([]rune(c)) > 500 {
		return nil, fmt.Errorf("content too long")
	}

	now := time.Now()
	m := &models.DirectMessage{
		FromUserID: fromUserID,
		ToUserID:   toUserID,
		SentAt:     now,
		Content:    c,
		ExpireAt:   now.Add(dmMessageTTL),
	}

	// 1) 先写数据库
	if err := s.dmRepo.CreateMessage(m); err != nil {
		return nil, err
	}

	// 2) 再删缓存（会话列表 + 会话消息）
	if s.redisRepo != nil {
		_ = s.redisRepo.InvalidatePeers(fromUserID)
		_ = s.redisRepo.InvalidatePeers(toUserID)
		_ = s.redisRepo.InvalidateConversation(fromUserID, toUserID)
		_ = s.redisRepo.InvalidateConversation(toUserID, fromUserID)
	}

	return m, nil
}
