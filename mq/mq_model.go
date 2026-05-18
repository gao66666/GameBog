package mq

type ArticleActionMsg struct {
	Type      string `json:"type"` // "like", "unlike", 	"view"
	ArticleID uint64 `json:"article_id"`
	UserID    uint64 `json:"user_id"`   // 仅点赞需要，阅读量可以忽略
	Timestamp int64  `json:"timestamp"` // 发生时间
	// LikeDelta 取消点赞时由 API 写入：-1 表示 Redis 已减展示计数；0 表示仅异步删 article_likes。
	// nil 表示旧版消息，消费者按 -1 处理以保持兼容。
	LikeDelta *int `json:"like_delta,omitempty"`
}

// CommentActionMsg 评论点赞消息
type CommentActionMsg struct {
	Type      string `json:"type"` // "like", "unlike"
	CommentID uint64 `json:"comment_id"`
	UserID    uint64 `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
}
