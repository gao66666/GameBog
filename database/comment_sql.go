package database

import (
	"github.com/gao66666/GoBlog/models"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type CommentRepository struct {
	db *gorm.DB
}

// 创建UserRepository实例
func NewCommentRepository(db *gorm.DB) *CommentRepository {
	return &CommentRepository{db: db}
}

// 初始化表结构
func (r *CommentRepository) InitTable() error {
	err := r.db.AutoMigrate(&models.Comment{})
	if err != nil {
		return ErrInitComment
	}
	zap.L().Info("评论表初始化成功")
	return nil
}

// 根据parentID返回一条评论
func (r *CommentRepository) GetCommentByID(id uint64) (*models.Comment, error) {
	var comment models.Comment
	// Select 指定字段（可选）：甚至可以只查 ID, UserID, RootID，性能更极致
	err := r.db.Select("id", "user_id", "root_id").
		Where("id = ?", id).
		First(&comment).Error

	if err != nil {
		return nil, err
	}

	return &comment, nil
}

// DeleteCommentByID 删除评论（仅按 id + user_id 约束，保证只能删自己的）。
func (r *CommentRepository) DeleteCommentByID(id uint64, userID uint64) error {
	if id == 0 || userID == 0 {
		return gorm.ErrInvalidData
	}
	return r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&models.Comment{}).Error
}

// CreateComment 将评论写入数据库
func (r *CommentRepository) CreateComment(comment *models.Comment) (*models.Comment, error) {
	// GORM 执行 Create 后，会把数据库生成的数据（如 CreatedAt）回填到 comment 指针指向的内存中
	err := r.db.Create(comment).Error

	if err != nil {
		zap.L().Error("db.Create comment failed", zap.Error(err))
		return nil, err
	}

	// 此时 comment 指向的对象已经被数据库信息“补全”了
	return comment, nil
}

// page是页数,表示第几页，size表示一页有多少楼，
// 先获取满足条件的rootID
func (r *CommentRepository) GetRootIDSByArticleID(articleID uint64, page int, size int) ([]uint64, error) {
	var rootIDs []uint64
	limit := size
	offset := (page - 1) * limit

	// 1. 直接 Pluck ID，不需要 Preload，也不需要 Find 整个结构体
	err := r.db.Model(&models.Comment{}).
		Where("article_id = ? AND root_id = 0", articleID).
		Order("created_at DESC"). // 保证分页顺序一致
		Offset(offset).
		Limit(limit).
		Pluck("id", &rootIDs).Error // 直接把 id 这一列扫到 rootIDs 切片里

	if err != nil {
		return nil, err
	}
	return rootIDs, nil
}

// 查询一层楼的所有评论
func (r *CommentRepository) GetCommentByRootID(rootID uint64, offset, limit int) ([]*models.Comment, int64, error) {
	var comments []*models.Comment
	var total int64

	// 1. 构造基础查询：包含楼长本身 (id = rootID) 和 所有子回复 (root_id = rootID)
	// 这样一次请求就能把楼长的话和下面的回复全部按需分页带走
	db := r.db.Model(&models.Comment{}).
		Where("id = ? OR root_id = ?", rootID, rootID)

	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 2. 排序逻辑：
	// 楼长（id = rootID）由于 ID 或时间通常是最小的，ASC 排序会自动让楼长排在第一行
	err := db.Preload("User").
		Preload("ReplyUser").
		Order("created_at ASC"). // 对话流按时间正序
		Offset(offset).Limit(limit).
		Find(&comments).Error

	return comments, total, err
}

// 获取指定rootids中的所有评论信息
func (r *CommentRepository) GetCommentsByRootIDs(rootIDs []uint64) ([]*models.Comment, error) {
	var comments []*models.Comment

	// 1. 构造查询条件：
	// id IN ?      -> 匹配楼长自己
	// root_id IN ? -> 匹配该楼层下的所有回复
	err := r.db.Preload("User"). // 关联评论者信息
					Preload("ReplyUser"). // 关联被回复者信息
					Where("id IN ? OR root_id IN ?", rootIDs, rootIDs).
		// 排序建议：先按 root_id 分组，组内按时间正序，方便 Service 逻辑组装
		Order("root_id ASC, created_at ASC").
		Find(&comments).Error

	if err != nil {
		zap.L().Error("批量查询评论失败", zap.Errors("err", []error{err}), zap.Uint64s("rootIDs", rootIDs))
		return nil, err
	}

	return comments, nil
}

// CountByArticleID 统计某篇文章下的所有评论数量（包含楼层和回复）。
func (r *CommentRepository) CountByArticleID(articleID uint64) (int64, error) {
	var total int64
	if err := r.db.Model(&models.Comment{}).
			Where("article_id = ?", articleID).
			Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// CountByArticleIDs 批量统计多篇文章的评论数（包含楼层和回复）。
// 返回 map[article_id]count，不在 map 中的视为 0。
func (r *CommentRepository) CountByArticleIDs(articleIDs []uint64) (map[uint64]int64, error) {
	out := make(map[uint64]int64)
	if len(articleIDs) == 0 {
		return out, nil
	}

	type row struct {
		ArticleID uint64
		Cnt       int64
	}
	rows := make([]row, 0, len(articleIDs))

	err := r.db.Model(&models.Comment{}).
		Select("article_id, COUNT(1) as cnt").
		Where("article_id IN ?", articleIDs).
		Group("article_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, r := range rows {
		out[r.ArticleID] = r.Cnt
	}
	return out, nil
}
