package slack

import (
	"context"
	"errors"
	"net/http"
	"strings"

	ch "github.com/gao66666/GoBlog/handler/channel"
)

var (
	errNotConfigured    = errors.New("slack not configured")
	errMissingChannelID = errors.New("missing slack channel_id in outbound meta")
)

// Adapter Slack Events API 通道适配器。
type Adapter struct {
	cfg    Config
	client *Client
}

func NewAdapter(cfg Config) *Adapter {
	if !cfg.Enabled() {
		return &Adapter{cfg: cfg}
	}
	return &Adapter{cfg: cfg, client: NewClient(cfg.BotToken)}
}

func (a *Adapter) Name() string { return ch.NameSlack }

func (a *Adapter) Enabled() bool { return a.cfg.Enabled() }

func (a *Adapter) HandleWebhook(ctx context.Context, in ch.WebhookInput) (ch.WebhookResult, error) {
	_ = ctx
	if !a.Enabled() {
		return ch.WebhookResult{HTTPStatus: http.StatusServiceUnavailable}, errNotConfigured
	}
	if err := VerifyRequest(in.Header, a.cfg.SigningSecret, in.RawBody); err != nil {
		return ch.WebhookResult{HTTPStatus: http.StatusUnauthorized}, err
	}

	msg, challenge, err := ParseInbound(in.RawBody, a.cfg.BotUserID)
	if err != nil {
		return ch.WebhookResult{HTTPStatus: http.StatusBadRequest}, err
	}
	if challenge != "" {
		return ch.WebhookResult{
			HTTPStatus: http.StatusOK,
			Body:       map[string]string{"challenge": challenge},
		}, nil
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
	channelID := strings.TrimSpace(out.Meta["channel_id"])
	if channelID == "" {
		return errMissingChannelID
	}
	return a.client.PostText(ctx, channelID, out.Text)
}

func toInbound(m *InboundMessage) *ch.Inbound {
	if m == nil {
		return nil
	}
	return &ch.Inbound{
		Channel:      ch.NameSlack,
		TenantID:     "slack",
		EventID:      m.EventID,
		ExternalUser: m.UserID,
		ChatKey:      m.ChannelID,
		Text:         m.Text,
		MessageKind:  m.MessageKind,
		IsGroup:      m.IsGroup,
		MentionBot:   m.MentionBot,
		Meta: map[string]string{
			"channel_id": m.ChannelID,
		},
	}
}
