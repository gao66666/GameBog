package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/mq"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// NotificationPusher 用于在 Kafka 不可用时的降级直推（仅对在线用户）。
// 返回 true 表示已成功推送。
type NotificationPusher interface {
	PushNotification(userID uint64, payload []byte) bool
}

type CommentService struct {
	commentRepo *database.CommentRepository
	articleRepo *database.ArticleRepository
	userRepo    *database.UserRepository
	redisRepo   *database.RedisCommentRepository

	notificationRepo  *database.NotificationRepository
	redisNotification *database.RedisNotificationRepository
	pusher            NotificationPusher
	pointsSvc         *PointsService
}

func NewCommentService(
	dataRepo *database.CommentRepository,
	dr *database.ArticleRepository,
	ur *database.UserRepository,
	rs *database.RedisCommentRepository,
	notifyRepo *database.NotificationRepository,
	notifyRedis *database.RedisNotificationRepository,
	pusher NotificationPusher,
	pointsSvc *PointsService,
) *CommentService {
	return &CommentService{
		commentRepo: dataRepo,
		articleRepo: dr,
		userRepo:    ur,
		redisRepo:   rs,

		notificationRepo:  notifyRepo,
		redisNotification: notifyRedis,
		pusher:            pusher,
		pointsSvc:         pointsSvc,
	}
}

func (s *CommentService) GetCommentByID(id uint64) (*models.Comment, error) {
	comment, err := s.commentRepo.GetCommentByID(id)
	return comment, err
}

// DeleteComment 仅允许删除自己的评论。
func (s *CommentService) DeleteComment(userID uint64, commentID uint64) error {
	if userID == 0 || commentID == 0 {
		return ErrInternalServer
	}

	comment, err := s.commentRepo.GetCommentByID(commentID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return ErrCommentNotFound
		}
		return err
	}

	if comment.UserID != userID {
		return ErrCommentNoPermission
	}

	if err := s.commentRepo.DeleteCommentByID(commentID, userID); err != nil {
		return err
	}

	// 如果删除的是 CY 评论，清理缓存
	if comment.CommentType == "cy" && s.redisRepo != nil {
		_ = s.redisRepo.DeleteUserCYList(userID)
	}
	return nil
}

func (s *CommentService) CreateComment(comment *models.Comment) (*models.Comment, error) {
	// 1. 持久化到数据库
	comment, err := s.commentRepo.CreateComment(comment)
	if err != nil {
		return nil, err
	}

	// 2. 触发异步推送（不阻塞主流程）
	go s.sendCommentNotification(comment)

	// 3. 评论积分奖励
	if s.pointsSvc != nil {
		if err := s.pointsSvc.EarnPoints(comment.UserID, "comment", comment.ID); err != nil {
			zap.L().Warn("评论积分发放失败", zap.Uint64("comment_id", comment.ID), zap.Error(err))
		}
	}

	return comment, nil
}

func (s *CommentService) sendCommentNotification(c *models.Comment) {
	var targetID uint64
	var msg string
	var msgType string

	// 1. 获取评论者（发件人）信息
	senderName := fmt.Sprintf("UID:%d", c.UserID) // 默认保底文案
	commenter, err := s.userRepo.GetUserByID(c.UserID)
	if err == nil && commenter.Name != "" {
		senderName = commenter.Name
	}

	// 2. 评论内容预览处理 (支持中文字符截断)
	contentPreview := c.Content
	runes := []rune(contentPreview)
	if len(runes) > 15 {
		contentPreview = string(runes[:15]) + "..."
	}

	// 3. 区分场景构造通知文案
	if c.ParentID != 0 {
		// --- 场景 A: 回复消息 ---
		if c.ReplyUserID == c.UserID {
			return
		} // 自己回自己不推送
		targetID = c.ReplyUserID
		msg = fmt.Sprintf("%s 回复了你：%s", senderName, contentPreview)
		msgType = "reply"
	} else {
		// --- 场景 B: 文章评论 ---
		article, err := s.articleRepo.GetArticleByID(c.ArticleID)
		if err != nil || article.AuthorID == c.UserID {
			return
		}

		targetID = article.AuthorID
		msg = fmt.Sprintf("%s 评论了你的文章《%s》:%s", senderName, article.Title, contentPreview)
		msgType = "comment"
	}

	// 4. 正式入队
	if err := mq.PublishNotification(targetID, c.UserID, senderName, msg, msgType); err != nil {
		zap.L().Warn("通知入队失败，执行降级逻辑",
			zap.Uint64("to_uid", targetID),
			zap.Error(err))

		// 降级：优先直推在线用户；否则落库 + 红点，等用户打开 /me 时同步离线未读。
		p := mq.NotificationPayload{
			EventID:    tool.GenerateID(),
			UserID:     targetID,
			SenderID:   c.UserID,
			SenderName: senderName,
			Content:    msg,
			Type:       msgType,
			CreatedAt:  time.Now().Unix(),
		}
		if s.pusher != nil {
			if b, e := json.Marshal(p); e == nil {
				if ok := s.pusher.PushNotification(targetID, b); ok {
					return
				}
			}
		}

		if s.redisNotification != nil {
			_ = s.redisNotification.SetHasUnread(targetID)
		}
		if s.notificationRepo != nil {
			n := &models.Notification{
				EventID:    p.EventID,
				UserID:     targetID,
				SenderID:   c.UserID,
				SenderName: senderName,
				Content:    msg,
				Type:       msgType,
				IsRead:     false,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}
			if e := s.notificationRepo.BatchCreateNotifications([]*models.Notification{n}); e != nil {
				zap.L().Warn("降级落库通知失败", zap.Uint64("to_uid", targetID), zap.Error(e))
			}
		}
	}
}

// 服务层拿取到的是用户所有的信息，但我要进行处理，只拿取CommentUser内的信息放入CommentVo
// limit表示获取每楼前多少个评论
func (s *CommentService) GetCommentByArticleID(aid uint64, page, size, limit int) ([]*models.CommentVO, int64, error) {
	// 1. 拿到这一页的 size 个楼长 ID
	rootIds, err := s.commentRepo.GetRootIDSByArticleID(aid, page, size)
	if err != nil || len(rootIds) == 0 {
		if err != nil {
			return nil, 0, err
		}
		// 没有任何评论
		return []*models.CommentVO{}, 0, nil
	}

	// 统计该文章下的所有评论数量（包含回复）
	total, err := s.commentRepo.CountByArticleID(aid)
	if err != nil {
		return nil, 0, err
	}

	// 2. 初始化 Map，并提前把“坑”占好
	rootVOMap := make(map[uint64]*models.CommentVO, len(rootIds))
	for _, id := range rootIds {
		// 先初始化空的 VO，这样子评论进来时，Map 里一定已经有这个 Key 了
		rootVOMap[id] = &models.CommentVO{Children: make([]*models.CommentVO, 0, limit)}
	}

	// 3. 一次批量查询
	allComments, err := s.commentRepo.GetCommentsByRootIDs(rootIds)
	if err != nil {
		return nil, 0, err
	}

	// 4. 真正的高手遍历：一次过
	fullMap := make(map[uint64]bool) // 记录哪些楼层已经塞满了
	for _, c := range allComments {
		if c.RootID == 0 {
			actualVO := s.convertToVO(c)
			actualVO.Children = rootVOMap[c.ID].Children
			rootVOMap[c.ID] = actualVO
		} else {
			// 1. 先查这个楼层是不是已经满了，满了直接跳过，连 convertToVO 都不做
			if fullMap[c.RootID] {
				continue
			}
			if parentVO, ok := rootVOMap[c.RootID]; ok {
				if len(parentVO.Children) < limit {
					parentVO.Children = append(parentVO.Children, s.convertToVO(c))
					// 2. 达到上限立即标记，后续同 root_id 的评论直接被上面的 continue 拦住
					if len(parentVO.Children) >= limit {
						fullMap[c.RootID] = true
					}
				}
			}
		}
	}

	// 5. 按 rootIds 顺序返回
	finalResult := make([]*models.CommentVO, 0, len(rootIds))
	for _, id := range rootIds {
		finalResult = append(finalResult, rootVOMap[id])
	}
	return finalResult, total, nil
}

func (s *CommentService) convertToVO(c *models.Comment) *models.CommentVO {
	vo := &models.CommentVO{
		ID:          c.ID, // 如果前端是 Web，这里建议转成 string
		RootID:      c.RootID,
		Content:     c.Content,
		CommentType: c.CommentType,
		User: models.CommentUser{
			UserID:   c.User.ID,
			UserName: c.User.Name,
			Avatar:   c.User.Avatar,
		},
		LikeCount: c.LikeCount,
		CreatedAt: c.CreatedAt,
	}

	// 只有当存在被回复者且 ReplyUser 确实被查询出来了才赋值
	if c.ReplyUserID != 0 && c.ReplyUser.ID != 0 {
		vo.ReplyUser = &models.CommentUser{
			UserID:   c.ReplyUser.ID,
			UserName: c.ReplyUser.Name,
		}
	}
	return vo
}

func (s *CommentService) GetCommentFloorDetails(rootID uint64, offset int, limit int) ([]*models.Comment, int64, error) {
	comments, total, err := s.commentRepo.GetCommentByRootID(rootID, offset, limit)
	return comments, total, err
}

// LikeComment 点赞/取消点赞评论（复用文章点赞模式）。
func (s *CommentService) LikeComment(commentID uint64, userID uint64, isCancel bool) error {
	if userID == 0 || commentID == 0 {
		return fmt.Errorf("invalid user_id/comment_id")
	}

	actionType := "like"
	if isCancel {
		actionType = "unlike"
	}

	// 1) 先落唯一行为记录，只在状态真正变化时才继续
	changed, err := s.commentRepo.EnsureCommentLikeState(userID, commentID, isCancel)
	if err != nil {
		zap.L().Warn("更新评论点赞状态失败",
			zap.Uint64("comment_id", commentID),
			zap.Uint64("user_id", userID),
			zap.Error(err))
		return err
	}
	if !changed {
		return nil
	}

	// 2) 更新 Redis 实时计数
	if actionType == "like" {
		if err := s.redisRepo.IncrStats(commentID); err != nil {
			return err
		}
	} else {
		if err := s.redisRepo.DecrStats(commentID); err != nil {
			return err
		}
	}

	// 3) 投递 NSQ 消息（用于 MySQL 批量落库）
	msg := mq.CommentActionMsg{
		Type:      actionType,
		CommentID: commentID,
		UserID:    userID,
		Timestamp: time.Now().Unix(),
	}
	if err := mq.PublishCommentAction(msg); err != nil {
		zap.L().Error("发送评论点赞消息到NSQ失败", zap.Uint64("cid", commentID), zap.Error(err))
		// MQ 发送失败：回滚 Redis 侧增量
		if actionType == "like" {
			_ = s.redisRepo.DecrStats(commentID)
		} else {
			_ = s.redisRepo.IncrStats(commentID)
		}
		return err
	}

	// 4) 被点赞 → 给评论作者加积分（仅在点赞时）
	if actionType == "like" && s.pointsSvc != nil {
		comment, err := s.commentRepo.GetCommentByID(commentID)
		if err == nil && comment != nil && comment.UserID != userID {
			if err := s.pointsSvc.EarnPoints(comment.UserID, "comment_liked", commentID); err != nil {
				zap.L().Warn("评论被点赞积分发放失败",
					zap.Uint64("comment_id", commentID),
					zap.Uint64("author_id", comment.UserID),
					zap.Error(err))
			}
		}
	}

	return nil
}

// --- CY 插眼 ---

// CreateCY 创建一条 CY 评论。
func (s *CommentService) CreateCY(comment *models.Comment) (*models.Comment, error) {
	if comment == nil {
		return nil, ErrInternalServer
	}
	comment.CommentType = "cy"

	saved, err := s.commentRepo.CreateComment(comment)
	if err != nil {
		return nil, err
	}

	// 清理 CY 列表缓存
	if s.redisRepo != nil {
		_ = s.redisRepo.DeleteUserCYList(comment.UserID)
	}
	return saved, nil
}

// GetMyCYList 获取当前用户的 CY 列表（Redis 缓存 → DB）。
func (s *CommentService) GetMyCYList(userID uint64) ([]*models.Comment, error) {
	if userID == 0 {
		return nil, ErrInternalServer
	}
	if s.redisRepo != nil {
		if cached, hit, err := s.redisRepo.GetUserCYList(userID); err == nil && hit {
			return cached, nil
		}
	}
	list, err := s.commentRepo.ListCYByUserID(userID)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []*models.Comment{}
	}
	if s.redisRepo != nil {
		_ = s.redisRepo.SetUserCYList(userID, list)
	}
	return list, nil
}

// ListMyCYForDisplay 当前用户的 CY 列表，附带文章标题（供个人中心展示）。
func (s *CommentService) ListMyCYForDisplay(userID uint64) ([]models.CYListItem, error) {
	list, err := s.GetMyCYList(userID)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return []models.CYListItem{}, nil
	}

	idSet := make(map[uint64]struct{})
	for _, c := range list {
		if c != nil && c.ArticleID != 0 {
			idSet[c.ArticleID] = struct{}{}
		}
	}
	ids := make([]uint64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}

	var articles []*models.Article
	if s.articleRepo != nil && len(ids) > 0 {
		if err := s.articleRepo.GetArticlesByIDs(ids, &articles); err != nil {
			zap.L().Warn("ListMyCYForDisplay batch article", zap.Error(err))
		}
	}
	titleByID := make(map[uint64]string, len(articles))
	for _, a := range articles {
		if a != nil {
			titleByID[a.ID] = a.Title
		}
	}

	out := make([]models.CYListItem, 0, len(list))
	for _, c := range list {
		if c == nil {
			continue
		}
		title := titleByID[c.ArticleID]
		if title == "" {
			title = "文章已删除或不可用"
		}
		out = append(out, models.CYListItem{
			CommentID:    c.ID,
			ArticleID:    c.ArticleID,
			ArticleTitle: title,
			Content:      c.Content,
			CreatedAt:    c.CreatedAt,
		})
	}
	return out, nil
}
