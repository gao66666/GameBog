package database

import (
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
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
	err := r.db.AutoMigrate(&models.Article{}, &models.ArticleLike{}, &models.Tag{})
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

// EnsureTags 确保 tags 存在：不存在则创建（按 name 唯一）。
// 返回值按入参顺序返回（去重、去空）。
func (r *ArticleRepository) EnsureTags(names []string) ([]models.Tag, error) {
	if len(names) == 0 {
		return []models.Tag{}, nil
	}

	clean := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		n := strings.TrimSpace(raw)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		clean = append(clean, n)
	}
	if len(clean) == 0 {
		return []models.Tag{}, nil
	}

	var existing []models.Tag
	if err := r.db.Where("name IN ?", clean).Find(&existing).Error; err != nil {
		return nil, err
	}

	existingMap := make(map[string]models.Tag, len(existing))
	for _, t := range existing {
		existingMap[t.Name] = t
	}

	toCreate := make([]models.Tag, 0)
	for _, n := range clean {
		if _, ok := existingMap[n]; ok {
			continue
		}
		toCreate = append(toCreate, models.Tag{Name: n})
	}
	if len(toCreate) > 0 {
		if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&toCreate).Error; err != nil {
			return nil, err
		}
	}

	var all []models.Tag
	if err := r.db.Where("name IN ?", clean).Find(&all).Error; err != nil {
		return nil, err
	}
	allMap := make(map[string]models.Tag, len(all))
	for _, t := range all {
		allMap[t.Name] = t
	}

	ordered := make([]models.Tag, 0, len(clean))
	for _, n := range clean {
		if t, ok := allMap[n]; ok {
			ordered = append(ordered, t)
		}
	}
	return ordered, nil
}

func (r *ArticleRepository) GetArticleByID(id uint64) (*models.Article, error) {
	var article models.Article
	// Preload 会自动关联查询出标签信息
	err := r.db.Preload("Tags").Where("id = ?", id).First(&article).Error
	if err != nil {
		return nil, err
	}
	return &article, nil
}

func (r *ArticleRepository) IncrementViewCount(id uint64, count int64) error {
	// 使用 UpdateColumn 的好处：它不会触发 GORM 的 BeforeUpdate 等钩子，执行效率更高
	return r.db.Model(&models.Article{}).
		Where("id = ?", id).
		UpdateColumn("view_count", gorm.Expr("view_count + ?", count)).
		Error
}

func (r *ArticleRepository) IncrementLikeCount(id uint64, count int64) error {
	return r.db.Model(&models.Article{}).
		Where("id = ?", id).
		UpdateColumn("like_count", gorm.Expr("like_count + ?", count)).
		Error
}

func (r *ArticleRepository) BatchIncrementStats(viewDeltas map[uint64]int, likeDeltas map[uint64]int) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for id, count := range viewDeltas {
			if count == 0 {
				continue
			}
			if err := tx.Model(&models.Article{}).
				Where("id = ?", id).
				UpdateColumn("view_count", gorm.Expr("view_count + ?", count)).Error; err != nil {
				return err
			}
		}

		for id, count := range likeDeltas {
			if count == 0 {
				continue
			}
			if err := tx.Model(&models.Article{}).
				Where("id = ?", id).
				UpdateColumn("like_count", gorm.Expr("like_count + ?", count)).Error; err != nil {
				return err
			}
		}

		return nil
	})
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

func (r *ArticleRepository) UpdateArticle(article *models.Article) error {
	// 仅允许作者本人更新标题和正文等核心字段
	return r.db.Model(&models.Article{}).
		Where("id = ? AND author_id = ?", article.ID, article.AuthorID).
		Updates(map[string]interface{}{
			"title":      article.Title,
			"summary":    article.Summary,
			"content":    article.Content,
			"updated_at": article.UpdatedAt,
		}).Error
}

func (r *ArticleRepository) UpdateArticleWithTags(article *models.Article) error {
	if article == nil || article.ID == 0 || article.AuthorID == 0 {
		return gorm.ErrInvalidData
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.Article{}).
			Where("id = ? AND author_id = ?", article.ID, article.AuthorID).
			Updates(map[string]interface{}{
				"title":      article.Title,
				"summary":    article.Summary,
				"content":    article.Content,
				"updated_at": article.UpdatedAt,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		// many2many: article_tags
		// 注意：Replace 会把不存在的关联清掉
		if err := tx.Model(&models.Article{ID: article.ID}).Association("Tags").Replace(article.Tags); err != nil {
			return err
		}
		return nil
	})
}
func (r *ArticleRepository) GetArticlesByIDs(ids []uint64, articles *[]*models.Article) error {
	return r.db.Where("id IN ?", ids).Find(articles).Error
}

func (r *ArticleRepository) ListArticleIDs(limit int) ([]uint64, error) {
	if limit <= 0 {
		limit = 10000
	}

	ids := make([]uint64, 0, limit)
	err := r.db.Model(&models.Article{}).
		Order("id ASC").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}

	return ids, nil
}

// EnsureLikeState 确保点赞状态（唯一行为记录）。
// isCancel=false: 尝试插入点赞记录；若已存在则无变化。
// isCancel=true : 尝试删除点赞记录；若不存在则无变化。
// 返回值 changed 表示这次调用是否真正改变了点赞状态。
func (r *ArticleRepository) EnsureLikeState(userID, articleID uint64, isCancel bool) (changed bool, err error) {
	if userID == 0 || articleID == 0 {
		return false, gorm.ErrInvalidData
	}

	if !isCancel {
		like := &models.ArticleLike{UserID: userID, ArticleID: articleID}
		res := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(like)
		return res.RowsAffected > 0, res.Error
	}

	res := r.db.Where("user_id = ? AND article_id = ?", userID, articleID).Delete(&models.ArticleLike{})
	return res.RowsAffected > 0, res.Error
}

// SearchArticlesFallback ES 不可用时的降级查询：简单 LIKE 匹配。
// 注意：这是兜底能力，不保证性能；线上应优先使用搜索引擎或增加专用索引。
func (r *ArticleRepository) SearchArticlesFallback(keyword string, limit int) ([]*models.Article, error) {
	if limit <= 0 {
		limit = 20
	}
	var articles []*models.Article
	like := "%" + keyword + "%"
	err := r.db.Model(&models.Article{}).
		Select("id", "author_id", "title", "summary", "created_at").
		Where("title LIKE ? OR summary LIKE ? OR content LIKE ?", like, like, like).
		Order("created_at DESC").
		Limit(limit).
		Find(&articles).Error
	return articles, err
}
