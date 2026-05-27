package slack

import (
	"os"
	"strings"
)

// Config Slack Events API（Bot Token + Signing Secret）。
type Config struct {
	BotToken      string
	SigningSecret string
	BotUserID     string // 群聊 @ 判定，如 U01234567
}

func LoadConfigFromEnv() Config {
	return Config{
		BotToken:      strings.TrimSpace(os.Getenv("SLACK_BOT_TOKEN")),
		SigningSecret: strings.TrimSpace(os.Getenv("SLACK_SIGNING_SECRET")),
		BotUserID:     strings.TrimSpace(os.Getenv("SLACK_BOT_USER_ID")),
	}
}

func (c Config) Enabled() bool {
	return c.BotToken != "" && c.SigningSecret != ""
}
