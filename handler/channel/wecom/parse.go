package wecom

import (
	"encoding/xml"
	"strconv"
	"strings"
)

// InboundMessage 企业微信应用 text 消息。
type InboundMessage struct {
	EventID  string
	FromUser string
	AgentID  string
	Text     string
	MsgType  string
	MsgID    string
}

type encryptedEnvelope struct {
	XMLName xml.Name `xml:"xml"`
	Encrypt string   `xml:"Encrypt"`
}

type textMessage struct {
	XMLName      xml.Name `xml:"xml"`
	FromUserName string   `xml:"FromUserName"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	MsgId        int64    `xml:"MsgId"`
	AgentID      int64    `xml:"AgentID"`
}

// ParseEncryptedEnvelope 从 POST 原始 XML 取出 Encrypt 字段。
func ParseEncryptedEnvelope(raw []byte) (string, error) {
	var env encryptedEnvelope
	if err := xml.Unmarshal(raw, &env); err != nil {
		return "", err
	}
	return strings.TrimSpace(env.Encrypt), nil
}

// ParseTextMessage 解析解密后的明文 XML。
func ParseTextMessage(raw []byte) (*InboundMessage, error) {
	var msg textMessage
	if err := xml.Unmarshal(raw, &msg); err != nil {
		return nil, err
	}
	msgType := strings.TrimSpace(msg.MsgType)
	fromUser := strings.TrimSpace(msg.FromUserName)
	if fromUser == "" {
		return nil, nil
	}
	eventID := strconv.FormatInt(msg.MsgId, 10)
	agentID := strconv.FormatInt(msg.AgentID, 10)
	if msgType != "text" {
		if msg.MsgId != 0 {
			return &InboundMessage{
				EventID:  eventID,
				FromUser: fromUser,
				AgentID:  agentID,
				MsgType:  msgType,
			}, nil
		}
		return nil, nil
	}
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return nil, nil
	}
	return &InboundMessage{
		EventID:  eventID,
		FromUser: fromUser,
		AgentID:  agentID,
		Text:     text,
		MsgType:  "text",
		MsgID:    eventID,
	}, nil
}
