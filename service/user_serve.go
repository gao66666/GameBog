package service

import (
	"errors"

	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"
)

var (
	ErrUserNotFound  = errors.New("用户不存在")
	ErrWrongPassword = errors.New("密码错误")
	ErrHash          = errors.New("哈希加密错误")
	ErrDataSelect    = errors.New("数据库查询出错")
	ErrDataInsert    = errors.New("数据库新增用户出错")
	ErrTokenGenerate = errors.New("用户Token生成失败")
)

type UserService struct {
	userRepo  *database.UserRepository
	redisRepo *database.RedisRepository
}

func NewUserService(dataRepo *database.UserRepository, rs *database.RedisRepository) *UserService {
	return &UserService{
		userRepo:  dataRepo,
		redisRepo: rs,
	}
}

func (se *UserService) Login(param models.ParamLogin) (*models.User, string, error) {
	//查询数据库

	user, err := se.userRepo.GetUserByID(param.UserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "", ErrUserNotFound
		}
		return nil, "", ErrDataSelect
	}

	// 2. 验证密码
	if !user.CheckPassword(param.PassWord) {
		return nil, "", ErrWrongPassword
	}

	// 生成JWT Token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  user.ID,
		"username": user.Name,
		"exp":      time.Now().Add(24 * time.Hour).Unix(), // 24小时过期
	})
	tokenString, err := token.SignedString([]byte("your-secret-key"))
	if err != nil {
		return nil, "", ErrTokenGenerate
	}

	return user, tokenString, nil
}

func (se *UserService) SignUp(param models.ParamSignUp) error {
	//分配一个userid
	id := database.GenerateID()
	new_user := &models.User{ID: id, Name: param.Username, Tel: param.Tel, Password: param.Password}
	err := se.userRepo.CreateUser(new_user)
	if err != nil {
		return err
	}
	return nil
}
