package feishu

import (
	"encoding/json"
	"strings"
)

// InboundMessage 从 im.message.receive_v1 解析出的用户消息。
type InboundMessage struct {
	EventID     string
	MessageID   string
	ChatID      string
	ChatType    string // p2p | group
	OpenID      string
	MessageType string
	Text        string
	MentionBot  bool
}

type eventEnvelope struct {
	Schema string `json:"schema"`
	Header struct {
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
		Token     string `json:"token"`
	} `json:"header"`
	Event struct {
		Message struct {
			MessageID   string `json:"message_id"`
			ChatID      string `json:"chat_id"`
			ChatType    string `json:"chat_type"`
			MessageType string `json:"message_type"`
			Content     string `json:"content"`
			Mentions    []struct {
				Name   string `json:"name"`
				ID     struct {
					OpenID string `json:"open_id"`
				} `json:"id"`
				Key string `json:"key"`
			} `json:"mentions"`
		} `json:"message"`
		Sender struct {
			SenderID struct {
				OpenID string `json:"open_id"`
			} `json:"sender_id"`
		} `json:"sender"`
	} `json:"event"`
	Challenge string `json:"challenge"`
	Type      string `json:"type"`
	Token     string `json:"token"`
}

// ParseInbound 解析事件体；非 im.message.receive_v1 返回 nil。
func ParseInbound(raw []byte, verificationToken string) (*InboundMessage, error) {
	var env eventEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.Type == "url_verification" {
		return nil, nil
	}
	if verificationToken != "" && env.Header.Token != "" && env.Header.Token != verificationToken {
		return nil, nil
	}
	if env.Header.EventType != "im.message.receive_v1" {
		return nil, nil
	}
	msg := env.Event.Message
	if strings.TrimSpace(msg.MessageID) == "" {
		return nil, nil
	}
	openID := strings.TrimSpace(env.Event.Sender.SenderID.OpenID)
	if openID == "" {
		return nil, nil
	}
	mentionBot := len(msg.Mentions) > 0
	if msg.MessageType != "text" {
		return &InboundMessage{
			EventID:     env.Header.EventID,
			MessageID:   msg.MessageID,
			ChatID:      msg.ChatID,
			ChatType:    msg.ChatType,
			OpenID:      openID,
			MessageType: msg.MessageType,
			MentionBot:  mentionBot,
		}, nil
	}
	text := extractTextContent(msg.Content)
	return &InboundMessage{
		EventID:     env.Header.EventID,
		MessageID:   msg.MessageID,
		ChatID:      msg.ChatID,
		ChatType:    msg.ChatType,
		OpenID:      openID,
		MessageType: "text",
		Text:        text,
		MentionBot:  mentionBot,
	}, nil
}

func extractTextContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var wrap struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(content), &wrap) == nil {
		return strings.TrimSpace(wrap.Text)
	}
	return content
}

