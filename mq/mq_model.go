package mq

type ArticleActionMsg struct {
	Type      string `json:"type"` // "like", "unlike", 	"view"
	ArticleID uint64 `json:"article_id"`
	UserID    uint64 `json:"user_id"`   // 仅点赞需要，阅读量可以忽略
	Timestamp int64  `json:"timestamp"` // 发生时间
}

// CommentActionMsg 评论点赞消息
type CommentActionMsg struct {
	Type      string `json:"type"` // "like", "unlike"
	CommentID uint64 `json:"comment_id"`
	UserID    uint64 `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
}

// PointsSettleMsg 积分结算消息
type PointsSettleMsg struct {
	TxnID       uint64 `json:"txn_id"`
	UserID      uint64 `json:"user_id"`
	Amount      int64  `json:"amount"`      // 正=增加，负=扣减
	RefType     string `json:"ref_type"`    // article / comment / like / checkin
	RefID       uint64 `json:"ref_id"`      // 业务记录ID
	Balance     int64  `json:"balance"`     // 更新后的余额
	Type        string `json:"type"`        // income / expense
	Description string `json:"description"` // 描述
}
