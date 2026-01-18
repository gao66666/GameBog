package models

type NoticeTask struct {
	Type       int8   `json:"type"`        // 1:评论, 2:点赞, 3:关注
	SenderID   uint64 `json:"sender_id"`   // 操作者
	ReceiverID uint64 `json:"receiver_id"` // 接收者
	TargetID   uint64 `json:"target_id"`   // 文章ID或评论ID
	Brief      string `json:"brief"`       // 消息摘要
}
