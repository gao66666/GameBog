package feishu

import (
	"os"
	"strings"
)

// Config 飞书自建应用（环境变量）。
type Config struct {
	AppID              string
	AppSecret          string
	VerificationToken  string
	EncryptKey         string
}

func LoadConfigFromEnv() Config {
	return Config{
		AppID:             strings.TrimSpace(os.Getenv("FEISHU_APP_ID")),
		AppSecret:         strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET")),
		VerificationToken: strings.TrimSpace(os.Getenv("FEISHU_VERIFICATION_TOKEN")),
		EncryptKey:        strings.TrimSpace(os.Getenv("FEISHU_ENCRYPT_KEY")),
	}
}

func (c Config) Enabled() bool {
	return c.AppID != "" && c.AppSecret != ""
}
