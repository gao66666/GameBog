package wecom

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	ch "github.com/gao66666/GoBlog/handler/channel"
)

// Config 企业微信自建应用回调。
type Config struct {
	CorpID  string
	AgentID string
	Secret  string
	Token   string
	AesKey  string
}

func LoadConfigFromEnv() Config {
	return Config{
		CorpID:  strings.TrimSpace(os.Getenv("WECOM_CORP_ID")),
		AgentID: strings.TrimSpace(os.Getenv("WECOM_AGENT_ID")),
		Secret:  strings.TrimSpace(os.Getenv("WECOM_SECRET")),
		Token:   strings.TrimSpace(os.Getenv("WECOM_TOKEN")),
		AesKey:  strings.TrimSpace(os.Getenv("WECOM_AES_KEY")),
	}
}

func (c Config) Enabled() bool {
	return c.CorpID != "" && c.Secret != "" && c.Token != "" && c.AesKey != "" && c.AgentID != ""
}

var (
	errNotConfigured = errors.New("wecom not configured")
	errMissingToUser = errors.New("missing wecom touser in outbound meta")
)

// Adapter 企业微信应用消息通道适配器。
type Adapter struct {
	cfg    Config
	crypt  *MsgCrypt
	client *Client
}

func NewAdapter(cfg Config) *Adapter {
	if !cfg.Enabled() {
		return &Adapter{cfg: cfg}
	}
	crypt, err := NewMsgCrypt(cfg.Token, cfg.AesKey, cfg.CorpID)
	if err != nil {
		return &Adapter{cfg: cfg}
	}
	return &Adapter{
		cfg:    cfg,
		crypt:  crypt,
		client: NewClient(cfg),
	}
}

func (a *Adapter) Name() string { return ch.NameWeCom }

func (a *Adapter) Enabled() bool {
	return a.cfg.Enabled() && a.crypt != nil && a.client != nil
}

func (a *Adapter) HandleWebhook(ctx context.Context, in ch.WebhookInput) (ch.WebhookResult, error) {
	_ = ctx
	if !a.Enabled() {
		return ch.WebhookResult{HTTPStatus: http.StatusServiceUnavailable}, errNotConfigured
	}
	q := in.Query
	sig := firstQuery(q, "msg_signature")
	ts := firstQuery(q, "timestamp")
	nonce := firstQuery(q, "nonce")

	if strings.EqualFold(in.Method, http.MethodGet) {
		echo := firstQuery(q, "echostr")
		if echo == "" {
			return ch.WebhookResult{HTTPStatus: http.StatusBadRequest}, errors.New("missing echostr")
		}
		plain, err := a.crypt.VerifyURL(sig, ts, nonce, echo)
		if err != nil {
			return ch.WebhookResult{HTTPStatus: http.StatusUnauthorized}, err
		}
		return ch.WebhookResult{HTTPStatus: http.StatusOK, PlainBody: plain}, nil
	}

	enc, err := ParseEncryptedEnvelope(in.RawBody)
	if err != nil || enc == "" {
		return ch.WebhookResult{HTTPStatus: http.StatusBadRequest}, errors.New("invalid wecom encrypted body")
	}
	plain, err := a.crypt.DecryptPOST(sig, ts, nonce, enc)
	if err != nil {
		return ch.WebhookResult{HTTPStatus: http.StatusUnauthorized}, err
	}
	msg, err := ParseTextMessage(plain)
	if err != nil {
		return ch.WebhookResult{HTTPStatus: http.StatusBadRequest}, err
	}
	if msg == nil {
		return ch.WebhookResult{HTTPStatus: http.StatusOK, PlainBody: "success"}, nil
	}
	return ch.WebhookResult{
		HTTPStatus: http.StatusOK,
		PlainBody:  "success",
		Async:      toInbound(a.cfg.CorpID, msg),
	}, nil
}

func (a *Adapter) Send(ctx context.Context, out ch.Outbound) error {
	if a.client == nil {
		return errNotConfigured
	}
	toUser := strings.TrimSpace(out.Meta["touser"])
	if toUser == "" {
		return errMissingToUser
	}
	return a.client.SendText(ctx, toUser, out.Text)
}

func toInbound(corpID string, m *InboundMessage) *ch.Inbound {
	if m == nil {
		return nil
	}
	return &ch.Inbound{
		Channel:      ch.NameWeCom,
		TenantID:     corpID,
		EventID:      m.EventID,
		ExternalUser: m.FromUser,
		ChatKey:      m.FromUser,
		Text:         m.Text,
		MessageKind:  m.MsgType,
		IsGroup:      false,
		MentionBot:   true,
		Meta: map[string]string{
			"touser": m.FromUser,
		},
	}
}

func firstQuery(q map[string][]string, key string) string {
	if q == nil {
		return ""
	}
	vals, ok := q[key]
	if !ok || len(vals) == 0 {
		return ""
	}
	return strings.TrimSpace(vals[0])
}
