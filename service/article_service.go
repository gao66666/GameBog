package service

import (
	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/mq"
	"go.uber.org/zap"
)

type ArticleService struct {
	articleDB *database.ArticleRepository
	redisRepo *database.RedisArticleRepository
}

func NewArticleService(dataRepo *database.ArticleRepository, rs *database.RedisArticleRepository) *ArticleService {
	return &ArticleService{
		articleDB: dataRepo,
		redisRepo: rs,
	}
}

func (s *ArticleService) CreateArticle(article *models.Article) error {
	s.articleDB.CreateArticle(article)
	return nil
}

func (s *ArticleService) GetArticle(id uint64) (*models.Article, error) {
	// 1. 从 MySQL 获取文章主体 (标题、正文等静态内容)
	article, err := s.articleDB.GetArticleByID(id)
	if err != nil {
		return nil, err
	}

	// 2. 异步发送阅读指令到 NSQ (不再直接操作数据库)
	// 生产者发出消息后，消费者会去更新 Redis 和 内存Map
	go func() {
		_ = mq.Publish("article_stats", mq.ArticleActionMsg{
			Type:      "view",
			ArticleID: id,
		})
	}()

	views, likes, err := s.redisRepo.GetStats(id)
	// 3. 获取实时统计数据 (优先读 Redis)
	// 我们在 Service 层将 Redis 里的最新数字“套”在文章对象上
	if err != nil || (views == 0 && likes == 0) {
		// 方案：从 MySQL 取出的值回填到 Redis
		views = int64(article.ViewCount)
		likes = int64(article.LikeCount)

		// 异步回写，不阻塞主流程
		go s.redisRepo.InitStats(id, views, likes)
	}

	// 3. 组装数据返回
	article.ViewCount = uint64(views)
	article.LikeCount = uint64(likes)
	return article, nil
}

func (s *ArticleService) LikeArticle(id uint64, isCancel bool) error {
	// 1. 确定动作类型
	actionType := "like"
	if isCancel {
		actionType = "unlike"
	}

	// 2. (可选但推荐) 实时抢跑更新 Redis 缓存和排行榜
	// 这样用户在消息还没被 NSQ 消费的这几十毫秒内刷新页面，也能看到最新数据
	go func() {
		if !isCancel {
			_ = s.redisRepo.IncrStats(id, "like")
			_ = s.redisRepo.UpdateLeaderboard(id, "like")
		} else {
			// 注意：取消点赞时 Redis 需要减 1，你需要给 RedisRepo 加个 DecrStats 方法
			_ = s.redisRepo.DecrStats(id, "like")
			_ = s.redisRepo.UpdateLeaderboardDecr(id, "like")
		}
	}()

	// 3. 构造 NSQ 消息，确保数据最终落库 MySQL
	msg := mq.ArticleActionMsg{
		Type:      actionType,
		ArticleID: id,
	}

	// 4. 发送到 NSQ
	// 异步系统会接手剩下的事：内存聚合、定时 Flush 到 MySQL
	err := mq.Publish("article_stats", msg)
	if err != nil {
		// 如果 NSQ 发送失败，这里需要记录错误
		zap.L().Error("发送点赞消息到NSQ失败", zap.Uint64("aid", id), zap.Error(err))
		return err
	}

	return nil
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

func (s *ArticleService) GetLeaderboard(actionType string) ([]*models.Article, error) {
	// 1. 从 Redis 拿到有序的 ID 列表 (例如: [105, 101, 202])
	ids, err := s.redisRepo.GetTopArticleIDs(actionType, 10)
	if err != nil || len(ids) == 0 {
		return nil, err
	}

	// 2. 从 MySQL 批量查询文章详情
	var articles []*models.Article
	if err := s.articleDB.GetArticlesByIDs(ids, &articles); err != nil {
		return nil, err
	}

	// 3. 重点：手动排序对齐
	// 因为 MySQL 返回的 articles 顺序可能是 [101, 105, 202] (按 ID 排序)
	// 我们需要把它转回 [105, 101, 202]

	// 先存入 map 方便 O(1) 查找
	articleMap := make(map[uint64]*models.Article)
	for _, a := range articles {
		articleMap[a.ID] = a
	}

	// 按照 ids 的顺序重新组装结果
	orderedArticles := make([]*models.Article, 0, len(articles))
	for _, id := range ids {
		if a, ok := articleMap[id]; ok {
			// 这里可以顺便把 Redis 里的实时计数值补上去 (可选)
			orderedArticles = append(orderedArticles, a)
		}
	}

	return orderedArticles, nil
}
