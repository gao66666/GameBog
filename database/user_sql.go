package database

import (
	"fmt"

	"errors"
	"strings"

	"github.com/gao66666/GoBlog/models"
	"gorm.io/gorm"
)

var (
	ErrCreatUser  = errors.New("数据库新增用户出错")
	ErrHash       = errors.New("密码哈希出错")
	ErrUserExists = errors.New("该电话号码已经注册")
)

// 用户Repository
type UserRepository struct {
	db *gorm.DB
}

// 创建UserRepository实例
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// 初始化表结构
func (r *UserRepository) InitTable() error {
	err := r.db.AutoMigrate(&models.User{})
	if err != nil {
		return err
	}
	fmt.Println("用户表初始化成功")
	return nil
}

// 创建用户
// 创建用户
func (r *UserRepository) CreateUser(user *models.User) error {
	// 1. 加密密码
	if err := user.HashPassword(); err != nil {
		return ErrHash
	}

	// 2. 存储到数据库
	result := r.db.Create(user)
	if result.Error != nil {
		// 如果是唯一约束错误
		if isDuplicateKeyError(result.Error) {
			return ErrUserExists
		}
		return ErrCreatUser
	}

	return nil
}

// 检查是否为唯一约束错误
func isDuplicateKeyError(err error) bool {
	// MySQL 重复键错误
	return strings.Contains(err.Error(), "Duplicate entry") ||
		strings.Contains(err.Error(), "1062")
}
func (r *UserRepository) GetUserByID(userid uint64) (*models.User, error) {
	var user models.User
	result := r.db.First(&user, userid)
	if result.Error != nil {
		return nil, result.Error // 返回原始错误
	}
	return &user, nil
}

func (r *UserRepository) VerifyUser(userid uint, plainPassword string) (*models.User, error) {
	var user models.User
	result := r.db.Where("email = ?", userid).First(&user)
	if result.Error != nil {
		return nil, fmt.Errorf("用户不存在: %w", result.Error)
	}

	// 验证密码
	if !user.CheckPassword(plainPassword) {
		return nil, fmt.Errorf("密码错误")
	}

	return &user, nil
}
