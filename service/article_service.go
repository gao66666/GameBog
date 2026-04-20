package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/cache"
	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/mq"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	articleSummaryMaxRunes = 100
)

func buildArticleSummary(content string) string {
	s := strings.TrimSpace(content)
	if s == "" {
		return ""
	}
	// 单行化 + 压缩空白，避免摘要出现大片换行/空格
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")

	r := []rune(s)
	if len(r) > articleSummaryMaxRunes {
		r = r[:articleSummaryMaxRunes]
	}
	return string(r)
}

func normalizeSummaryText(in string) string {
	// 摘要由用户输入：这里只做最小规范化，避免“看起来被覆盖”。
	// - 去掉首尾空白
	// - 统一换行符（不压缩空白、不强制单行）
	s := strings.TrimSpace(in)
	if s == "" {
		return ""
	}
	return strings.ReplaceAll(s, "\r\n", "\n")
}

const (
	hotKeyRefreshInterval = 30 * time.Second
	hotKeyTopN            = 200
	hotKeyMinScore        = 20
	hotKeySetTTL          = 2 * time.Minute
	articleReadLimitTTL   = 1 * time.Hour
	articleActionLimitTTL = 1 * time.Hour
)

type ArticleService struct {
	articleDB   *database.ArticleRepository
	userRepo    *database.UserRepository
	commentRepo *database.CommentRepository
	followRepo  *database.FollowRepository
	redisRepo   *database.RedisArticleRepository
	topicRepo   *database.TopicRepository
	guard       *cache.ArticleGuard
}

func (s *ArticleService) fillCommentCounts(articles []*models.Article) {
	if s == nil || s.commentRepo == nil || len(articles) == 0 {
		return
	}
	ids := make([]uint64, 0, len(articles))
	for _, a := range articles {
		if a == nil || a.ID == 0 {
			continue
		}
		ids = append(ids, a.ID)
	}
	if len(ids) == 0 {
		return
	}

	cm, err := s.commentRepo.CountByArticleIDs(ids)
	if err != nil {
		zap.L().Warn("批量统计评论数失败", zap.Error(err))
		return
	}
	for _, a := range articles {
		if a == nil {
			continue
		}
		if v, ok := cm[a.ID]; ok {
			a.CommentCount = v
		}
	}
}

func NewArticleService(dataRepo *database.ArticleRepository, userRepo *database.UserRepository, commentRepo *database.CommentRepository, followRepo *database.FollowRepository, rs *database.RedisArticleRepository, topicRepo *database.TopicRepository) *ArticleService {
	svc := &ArticleService{
		articleDB:   dataRepo,
		userRepo:    userRepo,
		commentRepo: commentRepo,
		followRepo:  followRepo,
		redisRepo:   rs,
		topicRepo:   topicRepo,
		guard:       cache.NewArticleGuard(1_000_000, 0.01),
	}
	svc.warmBloomFilter()
	svc.startHotKeyRefresher()
	return svc
}

func (s *ArticleService) startHotKeyRefresher() {
	go func() {
		ticker := time.NewTicker(hotKeyRefreshInterval)
		defer ticker.Stop()

		for {
			if err := s.redisRepo.RefreshHotArticlesFromLeaderboard("view", hotKeyTopN, hotKeyMinScore, hotKeySetTTL); err != nil {
				zap.L().Warn("刷新HotKey集合失败", zap.Error(err))
			}
			<-ticker.C
		}
	}()
}

func (s *ArticleService) warmBloomFilter() {
	ids, err := s.articleDB.ListArticleIDs(50000)
	if err != nil {
		zap.L().Warn("Bloom 预热失败，将降级为仅空值缓存策略", zap.Error(err))
		return
	}
	s.guard.Warm(ids)
	zap.L().Info("Bloom 预热完成", zap.Int("count", len(ids)))
}

func (s *ArticleService) getArticleWithGuard(id uint64, isHot bool) (*models.Article, error) {
	v, err := s.guard.DoLoad(fmt.Sprintf("article:%d", id), func() (interface{}, error) {
		locked, token, lockErr := s.redisRepo.AcquireArticleRebuildLock(id, 3*time.Second)
		if lockErr != nil {
			zap.L().Warn("获取缓存重建锁失败，回退直接查库", zap.Uint64("article_id", id), zap.Error(lockErr))
		}
		if locked {
			defer s.redisRepo.ReleaseArticleRebuildLock(id, token)
		}

		article, dbErr := s.articleDB.GetArticleByID(id)
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				_ = s.redisRepo.SetArticleNullCache(id)
			}
			return nil, dbErr
		}

		s.guard.Add(article.ID)
		if isHot {
			_ = s.redisRepo.SetHotArticleCache(id, article)
		} else {
			_ = s.redisRepo.SetArticleCache(id, article)
		}
		return article, nil
	})
	if err != nil {
		return nil, err
	}

	return v.(*models.Article), nil
}

func (s *ArticleService) rebuildArticleCacheAsync(id uint64) {
	go func() {
		locked, token, err := s.redisRepo.AcquireArticleRebuildLock(id, 5*time.Second)
		if err != nil || !locked {
			return
		}
		defer s.redisRepo.ReleaseArticleRebuildLock(id, token)

		article, dbErr := s.articleDB.GetArticleByID(id)
		if dbErr != nil {
			if errors.Is(dbErr, gorm.ErrRecordNotFound) {
				_ = s.redisRepo.SetArticleNullCache(id)
			}
			return
		}

		s.guard.Add(article.ID)
		_ = s.redisRepo.SetHotArticleCache(id, article)
	}()
}

func (s *ArticleService) CreateArticle(article *models.Article) error {
	if article != nil {
		// 摘要要求前端必填，这里仅做最小规范化，不再用正文截取覆盖
		article.Summary = normalizeSummaryText(article.Summary)

		// 话题校验：必须是有效话题；不传则回填默认话题
		if s.topicRepo != nil {
			now := time.Now()
			if article.CategoryID == 0 {
				defID, err := s.topicRepo.EnsureDefaultTopic()
				if err != nil {
					return err
				}
				article.CategoryID = defID
			} else {
				if _, err := s.topicRepo.GetActiveTopicByID(article.CategoryID, now); err != nil {
					return tool.NewBizError(400, 40001, "话题不存在或已过期")
				}
			}
		}

		// 处理 tags（handler 传入的 article.Tags 只带 Name）
		if len(article.Tags) > 0 {
			names := make([]string, 0, len(article.Tags))
			for _, t := range article.Tags {
				if strings.TrimSpace(t.Name) == "" {
					continue
				}
				names = append(names, strings.TrimSpace(t.Name))
			}
			if len(names) > 0 {
				tags, err := s.articleDB.EnsureTags(names)
				if err != nil {
					return err
				}
				article.Tags = tags
			}
		}
	}
	err := s.articleDB.CreateArticle(article)
	if err != nil {
		return err
	}
	s.guard.Add(article.ID)
	_ = s.redisRepo.SetArticleCache(article.ID, article)
	_ = s.redisRepo.AddLatestArticle(article.ID, article.CreatedAt)
	s.syncArticleToSearch(article)
	return nil
}

func (s *ArticleService) syncArticleToSearch(article *models.Article) {
	if article == nil {
		return
	}

	authorName := fmt.Sprintf("UID:%d", article.AuthorID)
	if s.userRepo != nil {
		if user, err := s.userRepo.GetUserByID(article.AuthorID); err == nil && user.Name != "" {
			authorName = user.Name
		}
	}

	tags := make([]string, 0, len(article.Tags))
	for _, tag := range article.Tags {
		tags = append(tags, tag.Name)
	}

	payload := &models.SearchSyncPayload{
		ID:         article.ID,
		Title:      article.Title,
		AuthorName: authorName,
		Tags:       tags,
		Summary:    article.Summary,
	}

	if err := mq.PublishSearchSync(payload); err != nil {
		zap.L().Warn("发送文章搜索同步消息失败", zap.Uint64("article_id", article.ID), zap.Error(err))
	}
}

func (s *ArticleService) GetArticle(id uint64) (*models.Article, error) {
	isHot, hotErr := s.redisRepo.IsHotArticle(id)
	if hotErr != nil {
		zap.L().Warn("HotKey判定失败，回退普通缓存策略", zap.Uint64("article_id", id), zap.Error(hotErr))
		isHot = false
	}

	// 1. 缓存优先
	var (
		cachedArticle *models.Article
		expired       bool
		hit           bool
		nullHit       bool
		err           error
	)
	if isHot {
		cachedArticle, expired, hit, nullHit, err = s.redisRepo.GetCachedArticleWithLogicalExpire(id)
	} else {
		cachedArticle, hit, nullHit, err = s.redisRepo.GetCachedArticle(id)
	}
	if err != nil {
		zap.L().Warn("读取文章缓存失败，降级直查DB", zap.Uint64("article_id", id), zap.Error(err))
	} else {
		if hit && nullHit {
			return nil, gorm.ErrRecordNotFound
		}
		if hit && cachedArticle != nil {
			if isHot && expired {
				s.rebuildArticleCacheAsync(id)
			}
			return cachedArticle, nil
		}
	}

	// 2. 防穿透/击穿后回源
	article, err := s.getArticleWithGuard(id, isHot)
	if err != nil {
		return nil, err
	}

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
	if isHot {
		_ = s.redisRepo.SetHotArticleCache(id, article)
	} else {
		_ = s.redisRepo.SetArticleCache(id, article)
	}
	return article, nil
}

func (s *ArticleService) ReadArticle(id uint64, actorKey string) (*models.Article, error) {
	article, err := s.GetArticle(id)
	if err != nil {
		return nil, err
	}

	if actorKey == "" {
		actorKey = "anonymous"
	}

	limitKey := fmt.Sprintf("article:read:limit:%d:%s", id, actorKey)
	first, err := s.redisRepo.MarkOnceByKey(limitKey, articleReadLimitTTL)
	if err != nil {
		zap.L().Warn("写入文章阅读幂等key失败", zap.Uint64("article_id", id), zap.String("actor_key", actorKey), zap.Error(err))
		return article, nil
	}
	if !first {
		return article, nil
	}

	if err := s.redisRepo.IncrStats(id, "view"); err != nil {
		_ = s.redisRepo.DeleteKey(limitKey)
		zap.L().Warn("文章阅读统计写入Redis失败", zap.Uint64("article_id", id), zap.Error(err))
		return nil, err
	}
	if err := s.redisRepo.UpdateLeaderboard(id, "view"); err != nil {
		_ = s.redisRepo.DecrStats(id, "view")
		_ = s.redisRepo.DeleteKey(limitKey)
		zap.L().Warn("文章阅读排行榜写入Redis失败", zap.Uint64("article_id", id), zap.Error(err))
		return nil, err
	}

	msg := mq.ArticleActionMsg{
		Type:      "view",
		ArticleID: id,
		Timestamp: time.Now().Unix(),
	}
	if err := mq.PublishAction(msg); err != nil {
		zap.L().Error("发送阅读消息到NSQ失败", zap.Uint64("article_id", id), zap.Error(err))
		_ = s.redisRepo.DecrStats(id, "view")
		_ = s.redisRepo.UpdateLeaderboardDecr(id, "view")
		_ = s.redisRepo.DeleteKey(limitKey)
		return nil, err
	}

	return article, nil
}

func (s *ArticleService) LikeArticle(id uint64, userID uint64, isCancel bool) error {
	if userID == 0 || id == 0 {
		return fmt.Errorf("invalid user_id/article_id")
	}

	actionType := "like"
	if isCancel {
		actionType = "unlike"
	}

	// 1) 先落“唯一行为记录”，只在状态真正变化时才继续更新计数与投递 MQ。
	changed, err := s.articleDB.EnsureLikeState(userID, id, isCancel)
	if err != nil {
		zap.L().Warn("更新点赞状态失败", zap.Uint64("article_id", id), zap.Uint64("user_id", userID), zap.Error(err))
		return err
	}
	if !changed {
		return nil
	}

	// 2) 状态变更后，再更新 Redis 实时计数与榜单（性能优先，可容忍短暂不一致）。
	if actionType == "like" {
		if err := s.redisRepo.IncrStats(id, "like"); err != nil {
			return err
		}
		if err := s.redisRepo.UpdateLeaderboard(id, "like"); err != nil {
			_ = s.redisRepo.DecrStats(id, "like")
			return err
		}
	} else {
		if err := s.redisRepo.DecrStats(id, "like"); err != nil {
			return err
		}
		if err := s.redisRepo.UpdateLeaderboardDecr(id, "like"); err != nil {
			_ = s.redisRepo.IncrStats(id, "like")
			return err
		}
	}

	// 3) 投递 NSQ 增量消息（用于 MySQL 批量落库）。
	msg := mq.ArticleActionMsg{
		Type:      actionType,
		ArticleID: id,
		UserID:    userID,
		Timestamp: time.Now().Unix(),
	}
	if err := mq.PublishAction(msg); err != nil {
		zap.L().Error("发送点赞消息到NSQ失败", zap.Uint64("aid", id), zap.Error(err))
		// MQ 发送失败：回滚 Redis 侧增量（DB 状态已变更，MySQL 计数后续可通过修正任务对齐）
		if actionType == "like" {
			_ = s.redisRepo.DecrStats(id, "like")
			_ = s.redisRepo.UpdateLeaderboardDecr(id, "like")
		} else {
			_ = s.redisRepo.IncrStats(id, "like")
			_ = s.redisRepo.UpdateLeaderboard(id, "like")
		}
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

	// 批量补齐评论数（用于列表/最新博客/个人中心展示）
	s.fillCommentCounts(articleList)

	return articleList, total, nil
}

// GetLatestArticlesAll 首页“最新博客”（全部）：
// - Redis ZSET 按时间倒序，仅保留最近 18 条（3 页 * 6 条）
// - 若 Redis 为空，则回源 DB 拉取最新 18 条并回填 Redis
func (s *ArticleService) GetLatestArticlesAll(page int, size int) ([]*models.Article, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 6
	}

	// 先读 Redis ZSET
	ids, total, err := s.redisRepo.GetLatestArticleIDs(page, size)
	if err != nil {
		zap.L().Warn("GetLatestArticleIDs failed, fallback to DB", zap.Error(err))
		ids = nil
		total = 0
	}

	// Redis 为空：回源 DB 拉取最新 18 条并回填
	if total == 0 {
		const keep = 18
		all, _, dbErr := s.articleDB.GetArticleList(0, 1, keep)
		if dbErr != nil {
			return nil, 0, dbErr
		}
		_ = s.redisRepo.SeedLatestArticles(all)

		// 只暴露最多 18 条，total 也按此计算（对应首页仅 3 页）
		total = int64(len(all))
		start := (page - 1) * size
		if start >= len(all) {
			return []*models.Article{}, total, nil
		}
		end := start + size
		if end > len(all) {
			end = len(all)
		}
		out := all[start:end]
		s.fillCommentCounts(out)
		return out, total, nil
	}

	// Redis 命中：批量查 DB，再按 ids 顺序组装
	if len(ids) == 0 {
		return []*models.Article{}, total, nil
	}

	var got []*models.Article
	if dbErr := s.articleDB.GetArticlesByIDs(ids, &got); dbErr != nil {
		return nil, 0, dbErr
	}

	m := make(map[uint64]*models.Article, len(got))
	for _, a := range got {
		if a == nil {
			continue
		}
		m[a.ID] = a
	}
	out := make([]*models.Article, 0, len(ids))
	for _, id := range ids {
		if a := m[id]; a != nil {
			out = append(out, a)
		}
	}

	s.fillCommentCounts(out)
	return out, total, nil
}

// GetFollowingLatestArticlesByView 仅关注：
// 拉取当前用户关注的人在近30天内发布的文章，并按阅读量倒序分页。
func (s *ArticleService) GetFollowingLatestArticlesByView(followerID uint64, page int, size int) ([]*models.Article, int64, error) {
	if followerID == 0 {
		return []*models.Article{}, 0, nil
	}
	if s.followRepo == nil {
		return []*models.Article{}, 0, nil
	}

	followingIDs, err := s.followRepo.GetFollowingIDs(followerID, 5000)
	if err != nil {
		return nil, 0, err
	}
	if len(followingIDs) == 0 {
		return []*models.Article{}, 0, nil
	}

	since := time.Now().AddDate(0, 0, -30)
	list, total, err := s.articleDB.GetArticlesByAuthorsSinceOrderByView(followingIDs, since, page, size)
	if err != nil {
		return nil, 0, err
	}

	s.fillCommentCounts(list)
	return list, total, nil
}

func (s *ArticleService) DeleteArticle(userID uint64, articleID uint64) error {
	// 只有作者本人才能删除文章
	return s.articleDB.DeleteArticle(articleID, userID)
}

func (s *ArticleService) UpdateArticle(userID uint64, articleID uint64, title, summary, content string, tagNames []string, categoryID uint) error {
	article, err := s.articleDB.GetArticleByID(articleID)
	if err != nil {
		return err
	}
	if article.AuthorID != userID {
		return fmt.Errorf("no permission to edit this article")
	}

	// 可选更新话题（0 表示保持不变）
	if categoryID != 0 {
		if s.topicRepo != nil {
			if _, err := s.topicRepo.GetActiveTopicByID(categoryID, time.Now()); err != nil {
				return tool.NewBizError(400, 40001, "话题不存在或已过期")
			}
		}
		article.CategoryID = categoryID
	}

	article.Title = title
	article.Summary = normalizeSummaryText(summary)
	article.Content = content
	article.UpdatedAt = time.Now()

	// 更新 tags
	if tagNames == nil {
		tagNames = []string{}
	}
	tags, err := s.articleDB.EnsureTags(tagNames)
	if err != nil {
		return err
	}
	article.Tags = tags

	if err := s.articleDB.UpdateArticleWithTags(article); err != nil {
		return err
	}

	// 写穿/失效 Redis 详情缓存，避免更新后仍读到旧内容
	if s.redisRepo != nil {
		isHot, hotErr := s.redisRepo.IsHotArticle(articleID)
		if hotErr == nil && isHot {
			_ = s.redisRepo.SetHotArticleCache(articleID, article)
		} else {
			_ = s.redisRepo.SetArticleCache(articleID, article)
		}
	}

	// 更新搜索索引（异步），避免搜索结果长期旧数据
	s.syncArticleToSearch(article)
	return nil
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

	// 批量补齐评论数
	s.fillCommentCounts(orderedArticles)

	return orderedArticles, nil
}
