package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/mq"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type FollowService struct {
	followRepo *database.FollowRepository
	userRepo   *database.UserRepository
	redisRepo  *database.RedisFollowRepository
	topicRepo  *database.TopicRepository
	userSvc    *UserService
	notifySink mq.NotifySink
}

func NewFollowService(fr *database.FollowRepository, ur *database.UserRepository, rs *database.RedisFollowRepository, tr *database.TopicRepository, userSvc *UserService, notifySink mq.NotifySink) *FollowService {
	return &FollowService{
		followRepo: fr,
		userRepo:   ur,
		redisRepo:  rs,
		topicRepo:  tr,
		userSvc:    userSvc,
		notifySink: notifySink,
	}
}

// CountFollowingUsers 我关注的用户人数（不含关注的话题）。
func (s *FollowService) CountFollowingUsers(userID uint64) (int64, error) {
	return s.followRepo.CountFollowingUsers(userID)
}

func (s *FollowService) CreateFollow(param *models.ParamFollow) error {
	// 1. 参数验证 (保持不变)
	if param.FollowerID == 0 || param.FollowingID == 0 {
		return ErrNullParamFollow
	}
	if param.FollowerID == param.FollowingID {
		return ErrFollowSelf
	}
	err := s.followRepo.CreateFollow(param)
	if err != nil {
		return err
	}

	// 3. 更新用户计数 (保持不变)
	if err := s.userRepo.IncrementFollowingCount(param.FollowingID); err != nil {
		zap.L().Error("更新粉丝数失败", zap.Uint64("user_id", param.FollowingID), zap.Error(err))
		return ErrUpdateUserCount
	}

	senderName := fmt.Sprintf("UID:%d", param.FollowerID)
	if follower, err := s.userRepo.GetUserByID(param.FollowerID); err == nil && follower.Name != "" {
		senderName = follower.Name
	}

	go func() {
		content := "你有一个新粉丝！"
		if s.notifySink != nil {
			s.notifySink.NotifyPushOrStore(param.FollowingID, param.FollowerID, senderName, content, "follow")
			return
		}
		if err := mq.PublishNotification(param.FollowingID, param.FollowerID, senderName, content, "follow"); err != nil {
			zap.L().Error("发送关注异步通知失败",
				zap.Uint64("target_id", param.FollowingID),
				zap.Error(err))
		}
	}()

	if s.userSvc != nil {
		s.userSvc.InvalidateUserSocialStats(param.FollowerID)
		s.userSvc.InvalidateUserSocialStats(param.FollowingID)
	}

	return nil
}

// --- TopicFollow ---

// FollowTopicByTopicID 关注话题（任意有效话题，含用户自建与游戏自动创建）。
func (s *FollowService) FollowTopicByTopicID(userID uint64, topicID uint) error {
	if userID == 0 || topicID == 0 {
		return ErrNullParamFollow
	}
	if s.topicRepo == nil {
		return ErrInternalServer
	}
	if _, err := s.topicRepo.GetActiveTopicByID(topicID, time.Now()); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tool.NewBizError(404, 40001, "话题不存在或已过期")
		}
		return err
	}
	if err := s.followRepo.CreateTopicFollow(userID, topicID); err != nil {
		return err
	}
	if s.redisRepo != nil {
		_ = s.redisRepo.DeleteTopicFollowList(userID)
	}
	return nil
}

// UnfollowTopicByTopicID 取消关注话题。
func (s *FollowService) UnfollowTopicByTopicID(userID uint64, topicID uint) error {
	if userID == 0 || topicID == 0 {
		return ErrNullParamFollow
	}
	if err := s.followRepo.DeleteTopicFollow(userID, topicID); err != nil {
		return err
	}
	if s.redisRepo != nil {
		_ = s.redisRepo.DeleteTopicFollowList(userID)
	}
	return nil
}

// ListFollowedTopics 获取用户关注的话题列表（Redis 缓存穿透）。
func (s *FollowService) ListFollowedTopics(userID uint64) ([]*models.TopicFollow, error) {
	if userID == 0 {
		return nil, ErrNullParamFollow
	}
	if s.redisRepo != nil {
		if cached, hit, err := s.redisRepo.GetTopicFollowList(userID); err == nil && hit {
			return cached, nil
		}
	}
	list, err := s.followRepo.ListTopicFollowByUser(userID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []*models.TopicFollow{}
	}
	if s.redisRepo != nil {
		_ = s.redisRepo.SetTopicFollowList(userID, list)
	}
	return list, nil
}

func (s *FollowService) IsTopicFollowed(userID uint64, topicID uint) (bool, error) {
	if userID == 0 || topicID == 0 {
		return false, nil
	}
	return s.followRepo.IsTopicFollowed(userID, topicID)
}

// ListFollowedTopicsWithNames 我关注的话题列表（带话题名）。
func (s *FollowService) ListFollowedTopicsWithNames(userID uint64) ([]models.TopicFollowListItem, error) {
	list, err := s.ListFollowedTopics(userID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []models.TopicFollowListItem{}, nil
	}

	ids := make([]uint, 0, len(list))
	seen := make(map[uint]struct{})
	for _, row := range list {
		if row == nil || row.TopicID == 0 {
			continue
		}
		if _, ok := seen[row.TopicID]; ok {
			continue
		}
		seen[row.TopicID] = struct{}{}
		ids = append(ids, row.TopicID)
	}

	nameByID := make(map[uint]string, len(ids))
	if s.topicRepo != nil && len(ids) > 0 {
		topics, terr := s.topicRepo.GetTopicsByIDs(ids)
		if terr != nil {
			zap.L().Warn("ListFollowedTopicsWithNames load topics", zap.Error(terr))
		}
		for _, t := range topics {
			if t != nil {
				nameByID[t.ID] = t.Name
			}
		}
	}

	out := make([]models.TopicFollowListItem, 0, len(list))
	for _, row := range list {
		if row == nil {
			continue
		}
		nm := nameByID[row.TopicID]
		if nm == "" {
			nm = "话题 #" + fmt.Sprintf("%d", row.TopicID)
		}
		out = append(out, models.TopicFollowListItem{
			UserID:    row.UserID,
			TopicID:   row.TopicID,
			TopicName: nm,
			CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}
