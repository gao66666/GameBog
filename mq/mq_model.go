package mq

type ArticleActionMsg struct {
	Type      string `json:"type"` // "like", "unlike", 	"view"
	ArticleID uint64 `json:"article_id"`
	UserID    uint64 `json:"user_id"`   // 仅点赞需要，阅读量可以忽略
	Timestamp int64  `json:"timestamp"` // 发生时间
}
