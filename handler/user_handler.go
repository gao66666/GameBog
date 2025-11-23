package handler

import (
	"log"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/service"
	"github.com/gin-gonic/gin"
)

func (h *UserHandler) LoginHandle(c *gin.Context) {
	var param models.ParamLogin
	if err := c.ShouldBindJSON(&param); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	//登录成功，用户不存在，登录密码错误、数据库查询失败
	user, token, err := h.se.Login(param)
	if err != nil {
		switch err {
		case service.ErrUserNotFound:
			c.JSON(404, gin.H{"error": "用户不存在"})
		case service.ErrWrongPassword:
			c.JSON(401, gin.H{"error": "密码错误"})
		default:
			log.Printf("登录系统错误: %v", err)
			c.JSON(500, gin.H{"error": "系统繁忙，请稍后重试"})
		}
		return
	}
	c.JSON(200, gin.H{
		"message":   "登录成功",
		"user_id":   user.ID,
		"user_name": user.Name,
		"token":     token,
	})
}

func (hd *UserHandler) SignUpHandle(c *gin.Context) {
	var param models.ParamSignUp
	if err := c.ShouldBindJSON(&param); err != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}

	err := hd.se.SignUp(param)
	if err != nil {
		switch err {
		case database.ErrHash:
			c.JSON(500, gin.H{"error": "系统错误，请稍后重试"}) // 内部错误用500
		case database.ErrUserExists:
			c.JSON(400, gin.H{"error": "该电话号码已经被注册"}) // 业务错误用400
		case database.ErrCreatUser:
			c.JSON(400, gin.H{"error": "数据库创建用户失败"}) // 业务错误用400
		default:
			c.JSON(500, gin.H{"error": "系统繁忙，请稍后重试"})
		}
		return
	}

	// 成功情况
	c.JSON(200, gin.H{
		"message": "用户注册成功",
		"user_id": 123, // 如果有的话
	})
}
