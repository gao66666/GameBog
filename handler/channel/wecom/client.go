package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	ch "github.com/gao66666/GoBlog/handler/channel"
)

const maxWeComTextRunes = 1800

// Client 企业微信应用消息 API。
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

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenExpire.Add(-60*time.Second)) {
		return c.token, nil
	}
	u := "https://qyapi.weixin.qq.com/cgi-bin/gettoken?corpid=" + url.QueryEscape(c.cfg.CorpID) +
		"&corpsecret=" + url.QueryEscape(c.cfg.Secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Errcode     int    `json:"errcode"`
		Errmsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Errcode != 0 || out.AccessToken == "" {
		return "", fmt.Errorf("wecom token: errcode=%d errmsg=%s", out.Errcode, out.Errmsg)
	}
	c.token = out.AccessToken
	if out.ExpiresIn <= 0 {
		out.ExpiresIn = 7200
	}
	c.tokenExpire = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return c.token, nil
}

func (c *Client) SendText(ctx context.Context, toUser, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "（暂无回复内容）"
	}
	agentID, err := strconv.Atoi(strings.TrimSpace(c.cfg.AgentID))
	if err != nil || agentID <= 0 {
		return fmt.Errorf("invalid WECOM_AGENT_ID")
	}
	for _, chunk := range ch.SplitTextRunes(text, maxWeComTextRunes) {
		if err := c.sendOnce(ctx, toUser, agentID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) sendOnce(ctx context.Context, toUser string, agentID int, text string) error {
	token, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{
		"touser":  toUser,
		"msgtype": "text",
		"agentid": agentID,
		"text":    map[string]string{"content": text},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://qyapi.weixin.qq.com/cgi-bin/message/send?access_token="+url.QueryEscape(token),
		bytes.NewReader(payload))
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
	var out struct {
		Errcode int    `json:"errcode"`
		Errmsg  string `json:"errmsg"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Errcode != 0 {
		return fmt.Errorf("wecom send: errcode=%d errmsg=%s", out.Errcode, out.Errmsg)
	}
	return nil
}
