package service

import (
	"fmt"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/mq"
	"go.uber.org/zap"
)

type FollowService struct {
	followRepo *database.FollowRepository
	userRepo   *database.UserRepository
	redisRepo  *database.RedisFollowRepository
}

func NewFollowService(fr *database.FollowRepository, ur *database.UserRepository, rs *database.RedisFollowRepository) *FollowService {
	return &FollowService{
		followRepo: fr,
		userRepo:   ur,
		redisRepo:  rs,
	}
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
		err := mq.PublishNotification(param.FollowingID, param.FollowerID, senderName, content, "follow")
		if err != nil {
			zap.L().Error("发送关注异步通知失败",
				zap.Uint64("target_id", param.FollowingID),
				zap.Error(err))
		}
	}()

	return nil
}
