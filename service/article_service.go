package service

import (
	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"go.uber.org/zap"
)

type ArticleService struct {
	articleDB *database.ArticleRepository
	redisRepo *database.RedisRepository
}

func NewArticleService(dataRepo *database.ArticleRepository, rs *database.RedisRepository) *ArticleService {
	return &ArticleService{
		articleDB: dataRepo,
		redisRepo: rs,
	}
}

func (s *ArticleService) CreateArticle(article *models.Article) error {
	s.articleDB.CreateArticle(article)
	return nil
}
func (s ArticleService) GetArticle(id uint64) (*models.Article, error) {
	article, err := s.articleDB.GetArticleByID(id)
	go func() {
		_ = s.articleDB.IncrementViewCount(id) //增加阅读量
	}()
	return article, err
}

func (s *ArticleService) GetArticleList(authorID uint64, page int, size int) ([]*models.Article, int64, error) {
	// 调用 Repo 层，拿到切片和总数
	articleList, total, err := s.articleDB.GetArticleList(authorID, page, size)
	if err != nil {
		zap.L().Error("Service GetArticleList failed", zap.Error(err))
		return nil, 0, err
	}

	return articleList, total, nil
}

func (s *ArticleService) DeleteArticle(id uint64, article_id uint64) error {
	//进行检验
	//删除
	err := s.articleDB.DeleteArticle(id, article_id)
	return err
}
