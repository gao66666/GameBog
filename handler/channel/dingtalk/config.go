package dingtalk

import (
	"os"
	"strings"
)

// Config 钉钉自定义机器人 / Outgoing 回调（SessionWebhook 回消息）。
type Config struct {
	AppSecret string // 机器人安全设置中的「加签」密钥；Outgoing 回调验签
}

func LoadConfigFromEnv() Config {
	return Config{
		AppSecret: strings.TrimSpace(os.Getenv("DINGTALK_APP_SECRET")),
	}
}

// Enabled 配置了加签密钥即启用（Outgoing 回调必须验签）。
func (c Config) Enabled() bool {
	return c.AppSecret != ""
}
