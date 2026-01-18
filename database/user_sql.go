package database

import (
	"fmt"

	"strings"

	"github.com/gao66666/GoBlog/models"
	"gorm.io/gorm"
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
		return ErrInitUser
	}
	fmt.Println("用户表初始化成功")
	return nil
}

// 创建用户
// 创建用户
func (r *UserRepository) CreateUser(user *models.User) error {

	//存储到数据库
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
		return nil, ErrUserGet // 返回原始错误
	}
	return &user, nil
}

func (r *UserRepository) UpdateUser(userID uint64, data map[string]interface{}) error {
	// 只有传入的 map 中存在的 key，SQL 才会更新对应的列
	return r.db.Model(&models.User{}).Where("id = ?", userID).Updates(data).Error
}
