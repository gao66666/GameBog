package service

import (
	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
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
	// 1. 参数验证
	if param.FollowerID == 0 || param.FollowingID == 0 {
		return ErrNullParamFollow
	}

	if param.FollowerID == param.FollowingID {
		return ErrFollowSelf
	}

	// 2. 执行关注
	err := s.followRepo.CreateFollow(param)
	if err != nil {
		return err
	}

	// 3. 更新用户计数
	// 更新被关注者的粉丝数
	if err := s.userRepo.IncrementFollowingCount(param.FollowingID); err != nil {
		// 这里可以考虑补偿操作，或者记录日志
		zap.L().Error("更新粉丝数失败",
			zap.Uint64("user_id", param.FollowingID),
			zap.Error(err))
		return ErrUpdateUserCount
	}

	// 更新关注者的关注数
	// if err := s.userRepo.IncrementFollowerCount(param.FollowerID); err != nil {
	// 	zap.L().Error("更新关注数失败",
	// 		zap.Uint64("user_id", param.FollowerID),
	// 		zap.Error(err))
	// 	return ErrUpdateUserCount
	// }

	return nil
}
