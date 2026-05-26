package wecom

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	ch "github.com/gao66666/GoBlog/handler/channel"
)

// Config 企业微信（预留，未实现收发）。
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
	return c.CorpID != "" && c.Secret != ""
}

// Adapter 企业微信适配器占位；启用配置后需实现 HandleWebhook / Send。
type Adapter struct {
	cfg Config
}

func NewAdapter(cfg Config) *Adapter {
	return &Adapter{cfg: cfg}
}

func (a *Adapter) Name() string { return ch.NameWeCom }

// Enabled 企微适配器尚未实现，保持 false 避免注册空壳路由。
func (a *Adapter) Enabled() bool { return false }

func (a *Adapter) HandleWebhook(ctx context.Context, in ch.WebhookInput) (ch.WebhookResult, error) {
	_ = ctx
	_ = in
	return ch.WebhookResult{HTTPStatus: http.StatusNotImplemented},
		errors.New("wecom adapter not implemented yet")
}

func (a *Adapter) Send(ctx context.Context, out ch.Outbound) error {
	_ = ctx
	_ = out
	return errors.New("wecom adapter not implemented yet")
}
