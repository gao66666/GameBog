package slack

import (
	"encoding/json"
	"strings"
)

// InboundMessage Slack Events API 归一化消息。
type InboundMessage struct {
	EventID     string
	ChannelID   string
	UserID      string
	ChannelType string // im | channel | group | mpim
	Text        string
	MessageKind string
	MentionBot  bool
	IsGroup     bool
}

type envelope struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	EventID   string `json:"event_id"`
	Event     struct {
		Type        string `json:"type"`
		User        string `json:"user"`
		Text        string `json:"text"`
		Channel     string `json:"channel"`
		ChannelType string `json:"channel_type"`
		BotID       string `json:"bot_id"`
		Subtype     string `json:"subtype"`
	} `json:"event"`
}

// ParseInbound 解析 url_verification / event_callback；非用户 text 消息返回 nil。
func ParseInbound(raw []byte, botUserID string) (*InboundMessage, string, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, "", err
	}
	if env.Type == "url_verification" {
		return nil, strings.TrimSpace(env.Challenge), nil
	}
	if env.Type != "event_callback" {
		return nil, "", nil
	}
	ev := env.Event
	if ev.Type != "message" {
		return nil, "", nil
	}
	if strings.TrimSpace(ev.BotID) != "" || ev.Subtype == "bot_message" {
		return nil, "", nil
	}
	userID := strings.TrimSpace(ev.User)
	channelID := strings.TrimSpace(ev.Channel)
	if userID == "" || channelID == "" {
		return nil, "", nil
	}
	channelType := strings.TrimSpace(ev.ChannelType)
	isGroup := channelType == "channel" || channelType == "group" || channelType == "mpim"
	text := strings.TrimSpace(ev.Text)
	mention := false
	if botUserID != "" {
		mention = strings.Contains(text, "<@"+botUserID+">")
		text = strings.TrimSpace(strings.ReplaceAll(text, "<@"+botUserID+">", ""))
	}
	if text == "" && isGroup {
		return &InboundMessage{
			EventID:     strings.TrimSpace(env.EventID),
			ChannelID:   channelID,
			UserID:      userID,
			ChannelType: channelType,
			MessageKind: "text",
			MentionBot:  mention,
			IsGroup:     isGroup,
		}, "", nil
	}
	if text == "" {
		return nil, "", nil
	}
	return &InboundMessage{
		EventID:     strings.TrimSpace(env.EventID),
		ChannelID:   channelID,
		UserID:      userID,
		ChannelType: channelType,
		Text:        text,
		MessageKind: "text",
		MentionBot:  mention,
		IsGroup:     isGroup,
	}, "", nil
}
