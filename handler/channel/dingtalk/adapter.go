package dingtalk

import (
	"context"
	"errors"
	"net/http"
	"strings"

	ch "github.com/gao66666/GoBlog/handler/channel"
)

var (
	errNotConfigured       = errors.New("dingtalk not configured")
	errMissingSessionHook  = errors.New("missing session_webhook in outbound meta")
)

// Adapter 钉钉 Outgoing 机器人通道适配器。
type Adapter struct {
	cfg    Config
	client *Client
}

func NewAdapter(cfg Config) *Adapter {
	if !cfg.Enabled() {
		return &Adapter{cfg: cfg}
	}
	return &Adapter{cfg: cfg, client: NewClient()}
}

func (a *Adapter) Name() string { return ch.NameDingTalk }

func (a *Adapter) Enabled() bool { return a.cfg.Enabled() }

func (a *Adapter) HandleWebhook(ctx context.Context, in ch.WebhookInput) (ch.WebhookResult, error) {
	_ = ctx
	if !a.Enabled() {
		return ch.WebhookResult{HTTPStatus: http.StatusServiceUnavailable}, errNotConfigured
	}
	if err := VerifyRequest(in.Header, a.cfg.AppSecret, in.RawBody); err != nil {
		return ch.WebhookResult{HTTPStatus: http.StatusUnauthorized}, err
	}
	msg, err := ParseInbound(in.RawBody)
	if err != nil {
		return ch.WebhookResult{HTTPStatus: http.StatusBadRequest}, err
	}
	if msg == nil {
		return ch.WebhookResult{HTTPStatus: http.StatusOK}, nil
	}
	return ch.WebhookResult{
		HTTPStatus: http.StatusOK,
		Async:      toInbound(msg),
	}, nil
}

func (a *Adapter) Send(ctx context.Context, out ch.Outbound) error {
	if a.client == nil {
		return errNotConfigured
	}
	hook := strings.TrimSpace(out.Meta["session_webhook"])
	if hook == "" {
		return errMissingSessionHook
	}
	return a.client.ReplyText(ctx, hook, out.Text)
}

func toInbound(m *InboundMessage) *ch.Inbound {
	if m == nil {
		return nil
	}
	chatKey := m.ConversationID
	if chatKey == "" {
		chatKey = m.SenderID
	}
	return &ch.Inbound{
		Channel:      ch.NameDingTalk,
		TenantID:     "dingtalk",
		EventID:      m.EventID,
		ExternalUser: m.SenderID,
		ChatKey:      chatKey,
		Text:         m.Text,
		MessageKind:  m.MessageKind,
		IsGroup:      m.IsGroup,
		MentionBot:   m.MentionBot,
		Meta: map[string]string{
			"session_webhook": m.SessionWebhook,
		},
	}
}
