package slack

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

const maxSlackTextRunes = 3800

// Client Slack Web API（chat.postMessage）。
type Client struct {
	token      string
	httpClient *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: strings.TrimSpace(token),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (c *Client) PostText(ctx context.Context, channelID, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "（暂无回复内容）"
	}
	for _, chunk := range ch.SplitTextRunes(text, maxSlackTextRunes) {
		if err := c.postOnce(ctx, channelID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) postOnce(ctx context.Context, channelID, text string) error {
	payload, _ := json.Marshal(map[string]string{
		"channel": channelID,
		"text":    text,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://slack.com/api/chat.postMessage", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if !out.OK {
		if out.Error == "" {
			out.Error = string(raw)
		}
		return fmt.Errorf("slack chat.postMessage: %s", out.Error)
	}
	return nil
}
