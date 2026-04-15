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
		return nil, result.Error
	}
	return &user, nil
}

// GetUserByTel 根据手机号查询用户
func (r *UserRepository) GetUserByTel(tel string) (*models.User, error) {
	var user models.User
	result := r.db.Where("tel = ?", tel).First(&user)
	if result.Error != nil {
		return nil, result.Error
	}
	return &user, nil
}

func (r *UserRepository) UpdateUser(userID uint64, data map[string]interface{}) error {
	// 只有传入的 map 中存在的 key，SQL 才会更新对应的列
	return r.db.Model(&models.User{}).Where("id = ?", userID).Updates(data).Error
}

func (r *UserRepository) IncrementFollowingCount(userID uint64) error {
	return r.db.Model(&models.User{}).
		Where("id = ?", userID).
		Update("following_count", gorm.Expr("following_count + 1")).Error
}

func (r *UserRepository) IncrementFollowerCount(userID uint64) error {
	return r.db.Model(&models.User{}).
		Where("id = ?", userID).
		Update("follower_count", gorm.Expr("follower_count + 1")).Error
}

// SearchUsersByNameOrTel 用于简单搜索用户，按名称/手机号模糊匹配，粗略代表“相关性”。
func (r *UserRepository) SearchUsersByNameOrTel(keyword string, limit int) ([]*models.User, error) {
	if limit <= 0 {
		limit = 20
	}

	var users []*models.User
	like := "%" + keyword + "%"
	err := r.db.Model(&models.User{}).
		Select("id", "name", "email", "tel", "avatar", "following_count", "created_at").
		Where("name LIKE ? OR tel LIKE ?", like, like).
		Order("name ASC").
		Limit(limit).
		Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}
