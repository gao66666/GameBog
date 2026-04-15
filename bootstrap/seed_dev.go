package bootstrap

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func seedDevData(db *gorm.DB) {
	if db == nil {
		return
	}

	// 仅在空库时塞入演示数据
	var userCnt int64
	if err := db.Model(&models.User{}).Count(&userCnt).Error; err != nil {
		zap.L().Warn("dev seed: count users failed", zap.Error(err))
		return
	}

	var artCnt int64
	if err := db.Model(&models.Article{}).Count(&artCnt).Error; err != nil {
		zap.L().Warn("dev seed: count articles failed", zap.Error(err))
		return
	}
	if userCnt > 0 || artCnt > 0 {
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	now := time.Now()

	userN := 10 + rng.Intn(6) // 10~15
	articlesPerUserMin := 3
	articlesPerUserMax := 5

	users := make([]*models.User, 0, userN)
	articles := make([]*models.Article, 0, userN*articlesPerUserMax)

	for i := 0; i < userN; i++ {
		u := &models.User{
			ID:       tool.GenerateID(),
			Name:     fmt.Sprintf("user_%02d", i+1),
			Tel:      fmt.Sprintf("139%08d", 10000000+i),
			Email:    "",
			Password: "123456",
			Avatar:   "",
		}
		if err := u.HashPassword(); err != nil {
			zap.L().Warn("dev seed: hash password failed", zap.Error(err))
			return
		}
		users = append(users, u)

		an := articlesPerUserMin + rng.Intn(articlesPerUserMax-articlesPerUserMin+1)
		for j := 0; j < an; j++ {
			createdAt := now.Add(-time.Duration(rng.Intn(72*60)) * time.Minute)
			a := &models.Article{
				ID:         tool.GenerateID(),
				AuthorID:   u.ID,
				Title:      fmt.Sprintf("%s 的第 %d 篇文章", u.Name, j+1),
				Summary:    "这是开发模式下自动生成的测试数据。",
				Content:    fmt.Sprintf("作者：%s\n\n这是一篇测试文章，用于验证：\n- 首页最新列表\n- 排行榜\n- 文章详情作者信息与关注按钮\n- 个人中心我的文章分页\n\n你可以在详情页发表评论。", u.Name),
				CategoryID: 0,
				CreatedAt:  createdAt,
				UpdatedAt:  createdAt,
			}
			articles = append(articles, a)
		}
	}

	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&users).Error; err != nil {
			return err
		}
		if err := tx.Create(&articles).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		if errors.Is(err, gorm.ErrInvalidData) {
			zap.L().Warn("dev seed: invalid article data", zap.Error(err))
			return
		}
		zap.L().Warn("dev seed: create users/articles failed", zap.Error(err))
		return
	}

	zap.L().Info("dev seed: demo users/articles created", zap.Int("users", len(users)), zap.Int("articles", len(articles)))
}
