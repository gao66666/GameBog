package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	feishuAPIBase      = "https://open.feishu.cn/open-apis"
	maxFeishuTextRunes = 3800
)

// Client 飞书 Open API（tenant_access_token + 发消息）。
type Client struct {
	cfg        Config
	httpClient *http.Client

	mu          sync.Mutex
	token       string
	tokenExpire time.Time
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) tenantAccessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpire.Add(-60*time.Second)) {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]string{
		"app_id":     c.cfg.AppID,
		"app_secret": c.cfg.AppSecret,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, feishuAPIBase+"/auth/v3/tenant_access_token/internal", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int    `json:"expire"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Code != 0 || out.TenantAccessToken == "" {
		return "", fmt.Errorf("feishu token: code=%d msg=%s", out.Code, out.Msg)
	}
	c.token = out.TenantAccessToken
	if out.Expire <= 0 {
		out.Expire = 7200
	}
	c.tokenExpire = time.Now().Add(time.Duration(out.Expire) * time.Second)
	return c.token, nil
}

// ReplyText 回复用户（单聊 open_id；群聊可用 chat_id）。
func (c *Client) ReplyText(ctx context.Context, receiveID, receiveIDType, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "（暂无回复内容）"
	}
	chunks := splitText(text, maxFeishuTextRunes)
	for _, chunk := range chunks {
		if err := c.sendTextOnce(ctx, receiveID, receiveIDType, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) sendTextOnce(ctx context.Context, receiveID, receiveIDType, text string) error {
	token, err := c.tenantAccessToken(ctx)
	if err != nil {
		return err
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	payload := map[string]string{
		"receive_id": receiveID,
		"msg_type":   "text",
		"content":    string(content),
	}
	body, _ := json.Marshal(payload)
	url := feishuAPIBase + "/im/v1/messages?receive_id_type=" + receiveIDType
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Code != 0 {
		return fmt.Errorf("feishu send: code=%d msg=%s body=%s", out.Code, out.Msg, string(raw))
	}
	return nil
}

func splitText(s string, maxRunes int) []string {
	if maxRunes <= 0 {
		return []string{s}
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return []string{s}
	}
	var out []string
	for i := 0; i < len(runes); i += maxRunes {
		end := i + maxRunes
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	return out
}
