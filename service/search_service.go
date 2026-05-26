package service

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/search"
	"go.uber.org/zap"
)

type SearchArticleResult struct {
	ID           uint64 `json:"id,string"`
	AuthorID     uint64 `json:"author_id,string"`
	AuthorName   string `json:"author_name"`
	Title        string `json:"title"`
	Summary      string `json:"summary"`
	ViewCount    uint64 `json:"viewCount"`
	LikeCount    uint64 `json:"likeCount"`
	CommentCount int64  `json:"commentCount"`
}

type SearchUserResult struct {
	ID   uint64 `json:"id,string"`
	Name string `json:"name"`
}

type GlobalSearchResult struct {
	Articles []SearchArticleResult `json:"articles"`
	Users    []SearchUserResult    `json:"users"`
}

type SearchService struct {
	articleRepo *database.ArticleRepository
	userRepo    *database.UserRepository
	commentRepo *database.CommentRepository
	searchRedis *database.RedisSearchRepository
}

func NewSearchService(
	articleRepo *database.ArticleRepository,
	userRepo *database.UserRepository,
	commentRepo *database.CommentRepository,
	searchRedis *database.RedisSearchRepository,
) *SearchService {
	return &SearchService{
		articleRepo: articleRepo,
		userRepo:    userRepo,
		commentRepo: commentRepo,
		searchRedis: searchRedis,
	}
}

func (s *SearchService) GlobalSearch(ctx context.Context, query string, page, size int) (*GlobalSearchResult, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 50 {
		size = 50
	}

	normQ := database.NormalizeSearchQuery(query)
	if normQ == "" {
		return &GlobalSearchResult{Articles: []SearchArticleResult{}, Users: []SearchUserResult{}}, nil
	}

	if s.searchRedis != nil {
		var cached GlobalSearchResult
		if hit, err := s.searchRedis.GetGlobalSearch(normQ, page, size, &cached); err != nil {
			zap.L().Warn("读取搜索缓存失败，降级直查", zap.Error(err))
		} else if hit {
			return &cached, nil
		}
	}

	result, err := s.searchUncached(ctx, query, page, size)
	if err != nil {
		return nil, err
	}

	if s.searchRedis != nil {
		if err := s.searchRedis.SetGlobalSearch(normQ, page, size, result); err != nil {
			zap.L().Warn("写入搜索缓存失败", zap.Error(err))
		}
	}
	return result, nil
}

func (s *SearchService) searchUncached(ctx context.Context, query string, page, size int) (*GlobalSearchResult, error) {
	var (
		userResults    []*models.User
		usersOut       []SearchUserResult
		articleResults any
	)

	if s.userRepo != nil {
		if users, err := s.userRepo.SearchUsersByNameOrTel(query, 10); err == nil {
			userResults = users
		} else {
			zap.L().Warn("用户搜索失败", zap.Error(err))
		}
	}

	if len(userResults) > 0 {
		usersOut = make([]SearchUserResult, 0, len(userResults))
		for _, u := range userResults {
			if u == nil || u.ID == 0 {
				continue
			}
			usersOut = append(usersOut, SearchUserResult{ID: u.ID, Name: u.Name})
		}
	}

	if !search.Enabled() {
		if s.articleRepo == nil {
			return &GlobalSearchResult{Articles: []SearchArticleResult{}, Users: usersOut}, nil
		}
		articles, err := s.articleRepo.SearchArticlesFallbackPaged(query, page, size)
		if err != nil {
			zap.L().Error("MySQL 搜索降级查询失败", zap.Error(err))
			return &GlobalSearchResult{Articles: []SearchArticleResult{}, Users: usersOut}, nil
		}
		articleResults = articles
	} else {
		hits, err := search.SearchArticlesPaged(ctx, query, page, size)
		if err != nil {
			zap.L().Error("Elasticsearch 搜索失败", zap.Error(err))
			if s.articleRepo != nil {
				articles, fbErr := s.articleRepo.SearchArticlesFallbackPaged(query, page, size)
				if fbErr == nil {
					articleResults = articles
				} else {
					zap.L().Warn("MySQL 搜索兜底也失败", zap.Error(fbErr))
				}
			}
		} else if len(hits) == 0 && s.articleRepo != nil {
			articles, fbErr := s.articleRepo.SearchArticlesFallbackPaged(query, page, size)
			if fbErr != nil {
				zap.L().Warn("ES 无命中后 MySQL 兜底失败", zap.Error(fbErr))
				articleResults = hits
			} else {
				articleResults = articles
			}
		} else {
			articleResults = hits
		}
	}

	if articleResults == nil {
		articleResults = []any{}
	}

	articlesOut, articleIDs := s.buildArticleResults(articleResults)
	s.enrichAuthorNames(articlesOut)
	s.enrichCommentCounts(articlesOut, articleIDs)

	if usersOut == nil {
		usersOut = []SearchUserResult{}
	}
	if articlesOut == nil {
		articlesOut = []SearchArticleResult{}
	}

	return &GlobalSearchResult{Articles: articlesOut, Users: usersOut}, nil
}

func (s *SearchService) buildArticleResults(articleResults any) ([]SearchArticleResult, []uint64) {
	articlesOut := make([]SearchArticleResult, 0)
	articleIDs := make([]uint64, 0)

	switch v := articleResults.(type) {
	case []*models.Article:
		for _, a := range v {
			if a == nil || a.ID == 0 {
				continue
			}
			articlesOut = append(articlesOut, SearchArticleResult{
				ID:        a.ID,
				AuthorID:  a.AuthorID,
				Title:     a.Title,
				Summary:   a.Summary,
				ViewCount: a.ViewCount,
				LikeCount: a.LikeCount,
			})
			articleIDs = append(articleIDs, a.ID)
		}
	case []map[string]any:
		ids := make([]uint64, 0, len(v))
		base := make(map[uint64]SearchArticleResult, len(v))
		for _, m := range v {
			rawID, ok := m["id"]
			if !ok {
				continue
			}
			var id uint64
			switch t := rawID.(type) {
			case string:
				parsed, _ := strconv.ParseUint(t, 10, 64)
				id = parsed
			case float64:
				id = uint64(t)
			case int64:
				id = uint64(t)
			case uint64:
				id = t
			}
			if id == 0 {
				continue
			}
			ids = append(ids, id)
			base[id] = SearchArticleResult{
				ID:      id,
				Title:   searchAsString(m["title"]),
				Summary: searchAsString(m["summary"]),
			}
		}

		if s.articleRepo != nil && len(ids) > 0 {
			var dbArticles []*models.Article
			if err := s.articleRepo.GetArticlesByIDs(ids, &dbArticles); err == nil {
				for _, a := range dbArticles {
					if a == nil || a.ID == 0 {
						continue
					}
					cur := base[a.ID]
					if cur.Title == "" {
						cur.Title = a.Title
					}
					if cur.Summary == "" {
						cur.Summary = a.Summary
					}
					if cur.AuthorID == 0 {
						cur.AuthorID = a.AuthorID
					}
					cur.ViewCount = a.ViewCount
					cur.LikeCount = a.LikeCount
					base[a.ID] = cur
				}
			}
		}

		for _, id := range ids {
			articlesOut = append(articlesOut, base[id])
			articleIDs = append(articleIDs, id)
		}
	}

	return articlesOut, articleIDs
}

func (s *SearchService) enrichAuthorNames(articlesOut []SearchArticleResult) {
	if s.userRepo == nil || len(articlesOut) == 0 {
		return
	}
	nameMap := make(map[uint64]string, 16)
	for i := range articlesOut {
		uid := articlesOut[i].AuthorID
		if uid == 0 {
			continue
		}
		if _, ok := nameMap[uid]; ok {
			continue
		}
		if u, err := s.userRepo.GetUserByID(uid); err == nil && u != nil {
			if u.Name != "" {
				nameMap[uid] = u.Name
			} else {
				nameMap[uid] = fmt.Sprintf("UID:%d", uid)
			}
		}
	}
	for i := range articlesOut {
		uid := articlesOut[i].AuthorID
		if uid == 0 {
			continue
		}
		if n, ok := nameMap[uid]; ok {
			articlesOut[i].AuthorName = n
		}
	}
}

func (s *SearchService) enrichCommentCounts(articlesOut []SearchArticleResult, articleIDs []uint64) {
	if s.commentRepo == nil || len(articleIDs) == 0 {
		return
	}
	cm, err := s.commentRepo.CountByArticleIDs(articleIDs)
	if err != nil {
		zap.L().Warn("批量统计评论数失败", zap.Error(err))
		return
	}
	for i := range articlesOut {
		if v, ok := cm[articlesOut[i].ID]; ok {
			articlesOut[i].CommentCount = v
		}
	}
}

func searchAsString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return fmt.Sprintf("%v", t)
	case int64:
		return strconv.FormatInt(t, 10)
	case uint64:
		return strconv.FormatUint(t, 10)
	default:
		return fmt.Sprint(t)
	}
}
