package database

import (
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrInitArticle   = tool.NewBizError(404, 40001, "文章数据库初始化出错")
	ErrCreatArticle  = tool.NewBizError(404, 40001, "数据库新增文章出错")
	ErrDeleteArticle = tool.NewBizError(404, 40001, "数据库删除文章出错")
)

type ArticleRepository struct {
	db *gorm.DB
}

func NewArticleRepository(db *gorm.DB) *ArticleRepository {
	return &ArticleRepository{db: db}
}

// 初始化表结构
func (r *ArticleRepository) InitTable() error {
	err := r.db.AutoMigrate(&models.Article{})
	if err != nil {
		return ErrInitArticle
	}
	zap.L().Info("文章数据库初始化成功")
	return nil
}

func (r *ArticleRepository) CreateArticle(article *models.Article) error {
	// 使用 r.db.Create 插入数据
	// GORM 会自动将结构体中的数据映射到 SQL 语句
	err := r.db.Create(article).Error
	if err != nil {
		zap.L().Error("创建文章失败", zap.Error(err))
		return ErrCreatArticle
	}
	return nil
}

func (r *ArticleRepository) GetArticleByID(id uint64) (*models.Article, error) {
	var article models.Article
	// Preload 会自动关联查询出作者信息和标签信息
	err := r.db.Preload("Tags").Where("id = ?", id).First(&article).Error
	if err != nil {
		return nil, err
	}
	return &article, nil
}

func (r *ArticleRepository) IncrementViewCount(id uint64) error {
	return r.db.Model(&models.Article{}).Where("id = ?", id).Update("view_count", gorm.Expr("view_count + ?", 1)).Error
}

func (r *ArticleRepository) GetArticleList(authorID uint64, page int, size int) ([]*models.Article, int64, error) {
	var articles []*models.Article
	var total int64

	query := r.db.Model(&models.Article{})

	if authorID > 0 {
		query = query.Where("author_id = ?", authorID)
	}

	// 1. 统计总数
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 2. 执行查询，加入 Order 方法
	err := query.Preload("Tags").
		// "created_at DESC" 表示按照创建时间 倒序（从大到小，即最新在先）
		// 如果想按 ID 倒序，可以写 "id DESC"
		Order("created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Find(&articles).Error

	return articles, total, err
}
func (r *ArticleRepository) DeleteArticle(articleID uint64, userID uint64) error {
	// 只有文章 ID 匹配 且 作者 ID 也匹配，才会真正执行删除
	// 这就是最简单且安全的权限校验
	result := r.db.Where("id = ? AND author_id = ?", articleID, userID).Delete(&models.Article{})

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return ErrDeleteArticle
	}

	return nil
}
