package service

import (
	"errors"
	"sync"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
)

// DeleteTemporaryTopic 仅允许删除临时话题
func (s *TopicService) DeleteTemporaryTopic(topicID uint) error {
	if s == nil || s.topicRepo == nil {
		return tool.NewBizError(500, 50001, "topic service unavailable")
	}
	t, err := s.topicRepo.GetActiveTopicByID(topicID, time.Now())
	if err != nil {
		return tool.NewBizError(404, 40001, "话题不存在或已过期")
	}
	if !t.IsTemporary {
		return tool.NewBizError(403, 40001, "长期话题不允许删除")
	}
	err = s.topicRepo.DeleteTopicCascade(topicID)
	if err == nil {
		s.invalidateTopicListCache()
	}
	return err
}

func (s *TopicService) invalidateTopicListCache() {
	if s != nil && s.topicRedis != nil {
		_ = s.topicRedis.InvalidateActiveTopicsListCache()
	}
}

// ListActiveTopicsPaged 分页获取可用话题
func (s *TopicService) ListActiveTopicsPaged(page, size int) ([]*models.Topic, int64, error) {
	if s == nil || s.topicRepo == nil {
		return []*models.Topic{}, 0, nil
	}
	if s.topicRedis != nil {
		if cached, total, hit, err := s.topicRedis.GetActiveTopicsPaged(page, size); err == nil && hit {
			return cached, total, nil
		} else if err != nil {
			zap.L().Warn("读取话题列表缓存失败，降级查库", zap.Error(err))
		}
	}
	list, total, err := s.topicRepo.GetActiveTopicsPaged(time.Now(), page, size)
	if err != nil {
		return nil, 0, err
	}
	if s.topicRedis != nil {
		if err := s.topicRedis.SetActiveTopicsPaged(page, size, list, total); err != nil {
			zap.L().Warn("写入话题列表缓存失败", zap.Error(err))
		}
	}
	return list, total, nil
}

const topicCleanupInterval = 1 * time.Minute

var topicCleanupOnce sync.Once

type TopicService struct {
	topicRepo   *database.TopicRepository
	articleRepo *database.ArticleRepository
	topicRedis  *database.RedisTopicRepository
}

func NewTopicService(topicRepo *database.TopicRepository, articleRepo *database.ArticleRepository, topicRedis *database.RedisTopicRepository) *TopicService {
	s := &TopicService{topicRepo: topicRepo, articleRepo: articleRepo, topicRedis: topicRedis}
	s.startCleanupLoop()
	return s
}

func (s *TopicService) startCleanupLoop() {
	if s == nil || s.topicRepo == nil {
		return
	}

	topicCleanupOnce.Do(func() {
		go func() {
			// 启动时先清理一轮
			if n, err := s.topicRepo.CleanupExpiredTemporaryTopics(time.Now(), 200); err == nil && n > 0 {
				zap.L().Info("cleanup expired topics", zap.Int("count", n))
			}

			ticker := time.NewTicker(topicCleanupInterval)
			defer ticker.Stop()
			for {
				<-ticker.C
				if n, err := s.topicRepo.CleanupExpiredTemporaryTopics(time.Now(), 200); err != nil {
					zap.L().Warn("cleanup expired topics failed", zap.Error(err))
				} else if n > 0 {
					zap.L().Info("cleanup expired topics", zap.Int("count", n))
				}
			}
		}()
	})
}

func (s *TopicService) ListActiveTopics() ([]*models.Topic, error) {
	if s == nil || s.topicRepo == nil {
		return []*models.Topic{}, nil
	}
	return s.topicRepo.GetActiveTopics(time.Now())
}

func (s *TopicService) GetActiveTopic(id uint) (*models.Topic, error) {
	if s == nil || s.topicRepo == nil {
		return nil, tool.NewBizError(500, 50001, "topic service unavailable")
	}
	return s.topicRepo.GetActiveTopicByID(id, time.Now())
}

// ErrTopicNoGameLink 表示话题未绑定游戏（无 game_topic_maps 记录）。
var ErrTopicNoGameLink = errors.New("topic has no linked game")

// GetLinkedGameID 话题若绑定游戏（game_topic_maps）则返回 game_id。
func (s *TopicService) GetLinkedGameID(topicID uint) (uint64, error) {
	if s == nil || s.topicRepo == nil || topicID == 0 {
		return 0, ErrTopicNoGameLink
	}
	return s.topicRepo.GetGameIDByTopicID(topicID)
}

func (s *TopicService) CreateTopic(name string, isTemporary bool) (*models.Topic, error) {
	if s == nil || s.topicRepo == nil {
		return nil, tool.NewBizError(500, 50001, "topic service unavailable")
	}
	topic, err := s.topicRepo.CreateTopic(name, isTemporary, time.Now())
	if err == nil {
		s.invalidateTopicListCache()
	}
	return topic, err
}

func (s *TopicService) ListTopicArticles(topicID uint, page int, size int) ([]*models.Article, int64, error) {
	if s == nil || s.articleRepo == nil {
		return []*models.Article{}, 0, nil
	}
	if s.topicRepo != nil {
		if _, err := s.topicRepo.GetActiveTopicByID(topicID, time.Now()); err != nil {
			return []*models.Article{}, 0, tool.NewBizError(400, 40001, "话题不存在或已过期")
		}
	}
	return s.articleRepo.GetArticlesByTopic(topicID, page, size)
}

func (s *TopicService) ListDiscussions(topicID uint, page int, size int) ([]*database.TopicDiscussionJoined, int64, error) {
	if s == nil || s.topicRepo == nil {
		return []*database.TopicDiscussionJoined{}, 0, nil
	}
	if _, err := s.topicRepo.GetActiveTopicByID(topicID, time.Now()); err != nil {
		return []*database.TopicDiscussionJoined{}, 0, tool.NewBizError(400, 40001, "话题不存在或已过期")
	}
	return s.topicRepo.ListDiscussions(topicID, page, size)
}

func (s *TopicService) CreateDiscussion(topicID uint, userID uint64, content string) error {
	if s == nil || s.topicRepo == nil {
		return tool.NewBizError(500, 50001, "topic service unavailable")
	}

	// topic 必须是有效/未过期的
	if _, err := s.topicRepo.GetActiveTopicByID(topicID, time.Now()); err != nil {
		return tool.NewBizError(400, 40001, "话题不存在或已过期")
	}

	d := &models.TopicDiscussion{
		ID:      tool.GenerateID(),
		TopicID: topicID,
		UserID:  userID,
		Content: content,
	}
	return s.topicRepo.CreateDiscussion(d)
}
