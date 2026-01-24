package service

import (
	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/models"
)

type CommentService struct {
	commentRepo *database.CommentRepository
	redisRepo   *database.RedisCommentRepository
}

func NewCommentService(dataRepo *database.CommentRepository, rs *database.RedisCommentRepository) *CommentService {
	return &CommentService{
		commentRepo: dataRepo,
		redisRepo:   rs,
	}
}

func (s *CommentService) GetCommentByID(id uint64) (*models.Comment, error) {
	comment, err := s.commentRepo.GetCommentByID(id)
	return comment, err
}

func (s *CommentService) CreateComment(comment *models.Comment) (*models.Comment, error) {
	comment, err := s.commentRepo.CreateComment(comment)
	return comment, err
}

// 服务层拿取到的是用户所有的信息，但我要进行处理，只拿取CommentUser内的信息放入CommentVo
// limit表示获取每楼前多少个评论
func (s *CommentService) GetCommentByArticleID(aid uint64, page, size, limit int) ([]*models.CommentVO, error) {
	// 1. 拿到这一页的 size 个楼长 ID
	rootIds, err := s.commentRepo.GetRootIDSByArticleID(aid, page, size)
	if err != nil || len(rootIds) == 0 {
		return nil, err
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
		return nil, err
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
	return finalResult, nil
}

func (s *CommentService) convertToVO(c *models.Comment) *models.CommentVO {
	vo := &models.CommentVO{
		ID:      c.ID, // 如果前端是 Web，这里建议转成 string
		RootID:  c.RootID,
		Content: c.Content,
		User: models.CommentUser{
			UserID:   c.User.ID,
			UserName: c.User.Name,
			Avatar:   c.User.Avatar,
		},
		LikeCount: c.LikeCount,
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
