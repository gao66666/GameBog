package dingtalk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// VerifyRequest 校验钉钉 Outgoing 机器人回调签名（timestamp + sign 请求头）。
func VerifyRequest(header http.Header, secret string, rawBody []byte) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	ts := strings.TrimSpace(header.Get("timestamp"))
	sign := strings.TrimSpace(header.Get("sign"))
	if ts == "" || sign == "" {
		return fmt.Errorf("missing dingtalk timestamp/sign headers")
	}
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid dingtalk timestamp")
	}
	if time.Since(time.UnixMilli(ms)) > 5*time.Minute || time.Until(time.UnixMilli(ms)) > 5*time.Minute {
		return fmt.Errorf("dingtalk timestamp out of range")
	}
	stringToSign := ts + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stringToSign))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sign)) {
		return fmt.Errorf("invalid dingtalk signature")
	}
	_ = rawBody
	return nil
}
