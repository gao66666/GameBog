package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	ch "github.com/gao66666/GoBlog/handler/channel"
)

const maxDingTalkTextRunes = 3800

// Client 通过 SessionWebhook 回复（Outgoing 机器人）。
type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) ReplyText(ctx context.Context, sessionWebhook, text string) error {
	webhook := strings.TrimSpace(sessionWebhook)
	if webhook == "" {
		return fmt.Errorf("empty session webhook")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		text = "（暂无回复内容）"
	}
	for _, chunk := range ch.SplitTextRunes(text, maxDingTalkTextRunes) {
		if err := c.sendOnce(ctx, webhook, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) sendOnce(ctx context.Context, webhook, text string) error {
	payload, _ := json.Marshal(map[string]any{
		"msgtype": "text",
		"text":    map[string]string{"content": text},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("dingtalk webhook: status=%d body=%s", resp.StatusCode, string(raw))
	}
	var out struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	if json.Unmarshal(raw, &out) == nil && out.Errcode != 0 {
		return fmt.Errorf("dingtalk webhook: errcode=%d errmsg=%s", out.Errcode, out.Errmsg)
	}
	return nil
}
