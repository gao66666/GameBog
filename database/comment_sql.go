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
