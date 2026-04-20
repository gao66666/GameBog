package service

import (
	"errors"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/jwt_module"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"

	"gorm.io/gorm"
)

type UserService struct {
	userRepo  *database.UserRepository
	redisRepo *database.RedisUserRepository
}

func NewUserService(dataRepo *database.UserRepository, rs *database.RedisUserRepository) *UserService {
	return &UserService{
		userRepo:  dataRepo,
		redisRepo: rs,
	}
}

func (se *UserService) Login(param models.ParamLogin) (*models.User, string, error) {
	// 1. 通过手机号查询用户
	user, err := se.userRepo.GetUserByTel(param.Tel)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", ErrUserNotFound
		}
		return nil, "", ErrDataSelect
	}

	// 2. 验证密码 (保持不变)
	if !user.CheckPassword(param.PassWord) {
		return nil, "", ErrWrongPassword
	}

	// 3. 使用你封装好的优雅方式生成 Token
	// 这样 Service 就不用关心过期时间、SecretKey 这些细节了
	tokenString, err := jwt_module.GenToken(user.ID, user.Name)
	if err != nil {
		// 记录日志，方便排查为什么签发失败
		zap.L().Error("jwt_module.GenToken failed", zap.Error(err), zap.Uint64("user_id", user.ID))
		return nil, "", ErrTokenGenerate
	}

	// 登录（上线）时预热一次用户基础信息缓存（2小时 TTL）。
	if se.redisRepo != nil {
		_ = se.redisRepo.SetUserBase(user)
	}

	return user, tokenString, nil
}

func (se *UserService) SignUp(param models.ParamSignUp) (uint64, error) {
	// 1. 分配用户ID
	id := tool.GenerateID()

	newUser := &models.User{
		ID:       id,
		Name:     param.Username,
		Tel:      param.Tel,
		Password: param.Password,
	}
	//对密码进行哈希处理
	if err := newUser.HashPassword(); err != nil {
		return 0, ErrHashGenerate
	}

	// 2. 写入数据库
	err := se.userRepo.CreateUser(newUser)
	if err != nil {
		// 判断是否是 Repository 抛出的“用户已存在”
		if errors.Is(err, database.ErrUserExists) {
			return 0, ErrUserAlreadyExists
		}
		// 其他写入失败情况返回 500
		return 0, ErrDataInsert
	}

	return id, nil
}

func (se *UserService) UpdateUser(userID uint64, data map[string]interface{}) error {
	// 先删除Redis缓存
	if se.redisRepo != nil {
		_ = se.redisRepo.DelUserBase(userID)
	}
	
	// 再更新数据库
	err := se.userRepo.UpdateUser(userID, data)
	if err != nil {
		return err
	}

	return nil
}

func (se *UserService) GetUserByID(userID uint64) (*models.User, error) {
	return se.userRepo.GetUserByID(userID)
}

// GetUserBaseCached 用于个人中心：优先读 Redis，miss 再读 DB 并回填缓存。
func (se *UserService) GetUserBaseCached(userID uint64) (*models.User, error) {
	if userID == 0 {
		return nil, ErrUserNotFound
	}
	if se.redisRepo != nil {
		   if cu, ok, err := se.redisRepo.GetUserBase(userID); err == nil && ok && cu != nil {
			   return &models.User{ID: userID, Name: cu.UserName, Email: cu.Email, Avatar: cu.Avatar, Github: cu.Github}, nil
		   }
	}

	u, err := se.userRepo.GetUserByID(userID)
	if err != nil {
		return nil, err
	}
	if se.redisRepo != nil {
		_ = se.redisRepo.SetUserBase(u)
	}
	return u, nil
}
