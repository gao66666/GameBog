package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	ch "github.com/gao66666/GoBlog/handler/channel"
)

var (
	errInvalidSignature = errors.New("invalid signature")
	errNotConfigured    = errors.New("feishu not configured")
	errMissingDelivery  = errors.New("missing receive_id in outbound meta")
)

// Adapter 飞书 IM 通道适配器。
type Adapter struct {
	cfg    Config
	client *Client
}

func NewAdapter(cfg Config) *Adapter {
	if !cfg.Enabled() {
		return &Adapter{cfg: cfg}
	}
	return &Adapter{cfg: cfg, client: NewClient(cfg)}
}

func (a *Adapter) Name() string { return ch.NameFeishu }

func (a *Adapter) Enabled() bool { return a.cfg.Enabled() }

func (a *Adapter) HandleWebhook(ctx context.Context, in ch.WebhookInput) (ch.WebhookResult, error) {
	_ = ctx
	raw := in.RawBody
	bodyStr := string(raw)

	if encKey := a.cfg.EncryptKey; encKey != "" {
		if sig := in.Header.Get("X-Lark-Signature"); sig != "" {
			ts := in.Header.Get("X-Lark-Request-Timestamp")
			nonce := in.Header.Get("X-Lark-Request-Nonce")
			if !VerifySignature(ts, nonce, encKey, bodyStr, sig) {
				return ch.WebhookResult{HTTPStatus: http.StatusUnauthorized}, errInvalidSignature
			}
		}
		var wrap struct {
			Encrypt string `json:"encrypt"`
		}
		if json.Unmarshal(raw, &wrap) == nil && strings.TrimSpace(wrap.Encrypt) != "" {
			plain, err := DecryptEventBody(encKey, wrap.Encrypt)
			if err != nil {
				return ch.WebhookResult{}, err
			}
			raw = plain
		}
	}

	if challenge, ok := ParseChallenge(raw, a.cfg); ok {
		return ch.WebhookResult{
			HTTPStatus: http.StatusOK,
			Body:       map[string]string{"challenge": challenge},
		}, nil
	}

	msg, err := ParseInbound(raw, a.cfg.VerificationToken)
	if err != nil {
		return ch.WebhookResult{HTTPStatus: http.StatusOK}, nil
	}
	if msg == nil {
		return ch.WebhookResult{HTTPStatus: http.StatusOK}, nil
	}

	inbound := toChannelInbound(a.cfg.AppID, msg)
	return ch.WebhookResult{
		HTTPStatus: http.StatusOK,
		Async:      inbound,
	}, nil
}

func (a *Adapter) Send(ctx context.Context, out ch.Outbound) error {
	if a.client == nil {
		return errNotConfigured
	}
	receiveID := out.Meta["receive_id"]
	receiveType := out.Meta["receive_id_type"]
	if receiveID == "" || receiveType == "" {
		return errMissingDelivery
	}
	return a.client.ReplyText(ctx, receiveID, receiveType, out.Text)
}

func toChannelInbound(appID string, m *InboundMessage) *ch.Inbound {
	if m == nil {
		return nil
	}
	receiveID := m.OpenID
	receiveType := "open_id"
	if strings.EqualFold(strings.TrimSpace(m.ChatType), "group") {
		receiveID = m.ChatID
		receiveType = "chat_id"
	}
	return &ch.Inbound{
		Channel:      ch.NameFeishu,
		TenantID:     appID,
		EventID:      m.EventID,
		ExternalUser: m.OpenID,
		ChatKey:      m.ChatID,
		Text:         m.Text,
		MessageKind:  m.MessageType,
		IsGroup:      strings.EqualFold(strings.TrimSpace(m.ChatType), "group"),
		MentionBot:   m.MentionBot,
		Meta: map[string]string{
			"receive_id":      receiveID,
			"receive_id_type": receiveType,
		},
	}
}
