package models

import (
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       uint64 `gorm:"primaryKey"`
	Name     string `gorm:"column:name;size:50"`
	Email    string `gorm:"column:email;size:100"`
	Tel      string `gorm:"column:tel;size:20;uniqueIndex"`
	Password string `gorm:"column:password;size:255"`
}

// ParamSignUp 注册请求参数
type ParamSignUp struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Tel      string `json:"tel" binding:"required"`
}

// ParamLogin 登录请求参数
type ParamLogin struct {
	UserID   uint64 `json:"userid" binding:"required"`
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

// 验证密码
func (u *User) CheckPassword(plainPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(plainPassword))
	return err == nil
}
