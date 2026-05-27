package models

import (
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID             uint64 `gorm:"primaryKey"`
	Name           string `gorm:"column:name;size:50"`
	Email          string `gorm:"column:email;size:100"`
	Tel            string `gorm:"column:tel;size:20;uniqueIndex"`
	Password       string `gorm:"column:password;size:255"`
	Avatar         string `gorm:"column:avatar"`          // 头像链接
	Github         string `gorm:"column:github;size:255"` // GitHub 主页链接
	FollowingCount uint64 `gorm:"column:following_count"` // 被多少人关注
	AccountBalance int64  `gorm:"column:account_balance;not null;default:500" json:"accountBalance"` // 账户余额（整数，默认 500；Redis 缓存见 user:account_balance）
	CreatedAt      time.Time
}

// ParamSignUp 注册请求参数
type ParamSignUp struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Tel      string `json:"tel" binding:"required"`
}

// ParamLogin 登录请求参数
type ParamLogin struct {
	Tel      string `json:"tel" binding:"required"`
	PassWord string `json:"password" binding:"required"`
}

func (u *User) HashPassword() error {
	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.Password = string(hashedBytes)
	return nil
}

// CheckPassword 验证密码
func (u *User) CheckPassword(plainPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(plainPassword))
	return err == nil
}

type UpdateUserParam struct {
	Name      *string `json:"name"`
	Tel       *string `json:"tel"`
	PassWord  *string `json:"password"`
	Email     *string `json:"email"`
	Avatar    *string `json:"avatar"`
	Github    *string `json:"github"`
	GithubURL *string `json:"github_url"`
	GithubUrl *string `json:"githubUrl"`
}
