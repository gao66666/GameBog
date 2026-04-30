package database

import (
	"errors"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 分页获取可用话题
func (r *TopicRepository) GetActiveTopicsPaged(now time.Time, page, size int) ([]*models.Topic, int64, error) {
	var topics []*models.Topic
	var total int64
	db := r.db.Model(&models.Topic{}).
		Where("(is_temporary = ? AND (expires_at IS NULL OR expires_at > ?)) OR (is_temporary = ?)", false, now, true)
	db.Count(&total)
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 6
	}
	db = db.Order("created_at DESC").Offset((page - 1) * size).Limit(size)
	if err := db.Find(&topics).Error; err != nil {
		return nil, 0, err
	}
	return topics, total, nil
}

var (
	ErrInitTopic        = tool.NewBizError(404, 40001, "话题数据库初始化出错")
	ErrInvalidTopicName = tool.NewBizError(400, 40001, "话题名称不正确")
)

const defaultTopicName = "默认"

type TopicRepository struct {
	db *gorm.DB
}

func NewTopicRepository(db *gorm.DB) *TopicRepository {
	return &TopicRepository{db: db}
}

func (r *TopicRepository) InitTable() error {
	if err := r.db.AutoMigrate(&models.Topic{}, &models.TopicDiscussion{}, &models.GameTopicMap{}); err != nil {
		return ErrInitTopic
	}
	if _, err := r.EnsureDefaultTopic(); err != nil {
		zap.L().Warn("ensure default topic failed", zap.Error(err))
	}
	zap.L().Info("话题数据库初始化成功")
	return nil
}

func (r *TopicRepository) EnsureGameTopic(gameID uint64, gameName string) (uint, error) {
	if gameID == 0 {
		return 0, gorm.ErrInvalidData
	}
	var mapped models.GameTopicMap
	if err := r.db.Where("game_id = ?", gameID).First(&mapped).Error; err == nil {
		return mapped.TopicID, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	name := strings.TrimSpace(gameName)
	if name == "" {
		name = "游戏话题"
	}
	topic, err := r.CreateTopic("游戏:"+name, false, time.Now())
	if err != nil {
		return 0, err
	}

	link := &models.GameTopicMap{GameID: gameID, TopicID: topic.ID}
	if err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "game_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"topic_id", "updated_at"}),
	}).Create(link).Error; err != nil {
		return 0, err
	}
	return topic.ID, nil
}

func (r *TopicRepository) GetTopicIDsByGameIDs(gameIDs []uint64) ([]uint, error) {
	if len(gameIDs) == 0 {
		return []uint{}, nil
	}
	var rows []models.GameTopicMap
	if err := r.db.Where("game_id IN ?", gameIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]uint, 0, len(rows))
	for _, row := range rows {
		if row.TopicID != 0 {
			out = append(out, row.TopicID)
		}
	}
	return out, nil
}

func (r *TopicRepository) EnsureDefaultTopic() (uint, error) {
	var t models.Topic
	err := r.db.Where("name = ? AND is_temporary = ?", defaultTopicName, false).First(&t).Error
	if err == nil {
		return t.ID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}

	toCreate := &models.Topic{Name: defaultTopicName, IsTemporary: false}
	// 并发情况下避免重复插入
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(toCreate).Error; err != nil {
		return 0, err
	}

	err = r.db.Where("name = ? AND is_temporary = ?", defaultTopicName, false).First(&t).Error
	if err != nil {
		return 0, err
	}
	return t.ID, nil
}

func (r *TopicRepository) GetActiveTopics(now time.Time) ([]*models.Topic, error) {
	var list []*models.Topic
	err := r.db.Model(&models.Topic{}).
		Where("is_temporary = ?", false).
		Or("is_temporary = ? AND expires_at > ?", true, now).
		Order("id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (r *TopicRepository) GetActiveTopicByID(id uint, now time.Time) (*models.Topic, error) {
	if id == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var t models.Topic
	err := r.db.Model(&models.Topic{}).
		Where("id = ?", id).
		Where("is_temporary = ? OR (is_temporary = ? AND expires_at > ?)", false, true, now).
		First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TopicRepository) CreateTopic(name string, isTemporary bool, now time.Time) (*models.Topic, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 50 {
		return nil, ErrInvalidTopicName
	}

	// 已存在则直接返回（避免重复）
	var existing models.Topic
	if err := r.db.Where("name = ?", name).First(&existing).Error; err == nil {
		// 如果存在但已过期：尝试立即级联清理，释放同名
		if existing.IsTemporary && existing.ExpiresAt != nil && !existing.ExpiresAt.After(now) {
			_ = r.DeleteTopicCascade(existing.ID)

			// 清理后再次确认是否仍占用同名
			var still models.Topic
			err2 := r.db.Where("name = ?", name).First(&still).Error
			if err2 == nil {
				return nil, tool.NewBizError(400, 40001, "该临时话题已过期，正在清理，请稍后再试")
			}
			if err2 != nil && !errors.Is(err2, gorm.ErrRecordNotFound) {
				return nil, err2
			}
			// fallthrough：允许创建新话题
		} else {
			return &existing, nil
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	var exp *time.Time
	if isTemporary {
		e := now.Add(48 * time.Hour)
		exp = &e
	}

	t := &models.Topic{Name: name, IsTemporary: isTemporary, ExpiresAt: exp}
	if err := r.db.Create(t).Error; err != nil {
		return nil, err
	}
	return t, nil
}

type TopicDiscussionJoined struct {
	ID        uint64    `json:"id,string"`
	TopicID   uint      `json:"topicId"`
	UserID    uint64    `json:"userId,string"`
	UserName  string    `json:"userName"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

func (r *TopicRepository) CreateDiscussion(d *models.TopicDiscussion) error {
	if d == nil || d.ID == 0 || d.TopicID == 0 || d.UserID == 0 {
		return gorm.ErrInvalidData
	}
	content := strings.TrimSpace(d.Content)
	if content == "" || len([]rune(content)) > 500 {
		return tool.NewBizError(400, 40001, "讨论内容不正确")
	}
	d.Content = content
	return r.db.Create(d).Error
}

func (r *TopicRepository) ListDiscussions(topicID uint, page int, size int) ([]*TopicDiscussionJoined, int64, error) {
	if topicID == 0 {
		return []*TopicDiscussionJoined{}, 0, nil
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}

	var total int64
	base := r.db.Table("topic_discussions td").Where("td.topic_id = ?", topicID)
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []*TopicDiscussionJoined
	err := base.
		Select("td.id, td.topic_id, td.user_id, td.content, td.created_at, u.name as user_name").
		Joins("LEFT JOIN users u ON u.id = td.user_id").
		Order("td.created_at DESC").
		Offset((page - 1) * size).
		Limit(size).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *TopicRepository) DeleteTopicCascade(topicID uint) error {
	if topicID == 0 {
		return gorm.ErrInvalidData
	}

	return r.db.Transaction(func(tx *gorm.DB) error {
		// 1) 删除话题下文章相关数据
		var articleIDs []uint64
		if err := tx.Model(&models.Article{}).Where("category_id = ?", topicID).Pluck("id", &articleIDs).Error; err != nil {
			return err
		}

		if len(articleIDs) > 0 {
			if err := tx.Where("article_id IN ?", articleIDs).Delete(&models.Comment{}).Error; err != nil {
				return err
			}
			if err := tx.Where("article_id IN ?", articleIDs).Delete(&models.ArticleLike{}).Error; err != nil {
				return err
			}
			if err := tx.Exec("DELETE FROM article_tags WHERE article_id IN ?", articleIDs).Error; err != nil {
				return err
			}
		}

		if err := tx.Where("category_id = ?", topicID).Delete(&models.Article{}).Error; err != nil {
			return err
		}

		// 2) 删除讨论区
		if err := tx.Where("topic_id = ?", topicID).Delete(&models.TopicDiscussion{}).Error; err != nil {
			return err
		}

		// 3) 删除话题本身
		if err := tx.Delete(&models.Topic{}, topicID).Error; err != nil {
			return err
		}

		return nil
	})
}

// CleanupExpiredTemporaryTopics 清理已过期的临时话题，并级联删除其文章与讨论。
// limit<=0 表示不限制。
func (r *TopicRepository) CleanupExpiredTemporaryTopics(now time.Time, limit int) (int, error) {
	var expired []*models.Topic
	q := r.db.Model(&models.Topic{}).
		Where("is_temporary = ? AND expires_at <= ?", true, now).
		Order("expires_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&expired).Error; err != nil {
		return 0, err
	}

	count := 0
	for _, t := range expired {
		if t == nil || t.ID == 0 {
			continue
		}
		if err := r.DeleteTopicCascade(t.ID); err != nil {
			zap.L().Warn("cleanup expired topic failed", zap.Uint("topic_id", t.ID), zap.Error(err))
			continue
		}
		count += 1
	}
	return count, nil
}
