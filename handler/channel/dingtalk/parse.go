package dingtalk

import (
	"encoding/json"
	"strings"
)

// InboundMessage 钉钉 Outgoing 机器人回调消息。
type InboundMessage struct {
	EventID        string
	SenderID       string
	ConversationID string
	SessionWebhook string
	Text           string
	MessageKind    string
	IsGroup        bool
	MentionBot     bool
}

type callbackBody struct {
	Msgtype        string `json:"msgtype"`
	MsgId          string `json:"msgId"`
	ConversationId string `json:"conversationId"`
	ConversationType string `json:"conversationType"` // 1 单聊 2 群聊
	SenderId       string `json:"senderId"`
	SessionWebhook string `json:"sessionWebhook"`
	IsInAtList     bool   `json:"isInAtList"`
	Text           struct {
		Content string `json:"content"`
	} `json:"text"`
}

// ParseInbound 解析 Outgoing 机器人 text 回调。
func ParseInbound(raw []byte) (*InboundMessage, error) {
	var body callbackBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if body.Msgtype != "text" {
		if body.MsgId != "" {
			return &InboundMessage{
				EventID:        body.MsgId,
				SenderID:       body.SenderId,
				ConversationID: body.ConversationId,
				SessionWebhook: body.SessionWebhook,
				MessageKind:    body.Msgtype,
				IsGroup:        body.ConversationType == "2",
				MentionBot:     body.IsInAtList,
			}, nil
		}
		return nil, nil
	}
	text := strings.TrimSpace(body.Text.Content)
	if text == "" {
		return nil, nil
	}
	isGroup := body.ConversationType == "2"
	return &InboundMessage{
		EventID:        strings.TrimSpace(body.MsgId),
		SenderID:       strings.TrimSpace(body.SenderId),
		ConversationID: strings.TrimSpace(body.ConversationId),
		SessionWebhook: strings.TrimSpace(body.SessionWebhook),
		Text:           text,
		MessageKind:    "text",
		IsGroup:        isGroup,
		MentionBot:     body.IsInAtList || !isGroup,
	}, nil
}
