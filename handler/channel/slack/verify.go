package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const signatureVersion = "v0"

// VerifyRequest 校验 Slack 请求签名（X-Slack-Signature / X-Slack-Request-Timestamp）。
func VerifyRequest(header http.Header, signingSecret string, rawBody []byte) error {
	signingSecret = strings.TrimSpace(signingSecret)
	if signingSecret == "" {
		return fmt.Errorf("slack signing secret not configured")
	}
	ts := strings.TrimSpace(header.Get("X-Slack-Request-Timestamp"))
	sig := strings.TrimSpace(header.Get("X-Slack-Signature"))
	if ts == "" || sig == "" {
		return fmt.Errorf("missing slack signature headers")
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid slack timestamp")
	}
	if time.Since(time.Unix(sec, 0)) > 5*time.Minute || time.Until(time.Unix(sec, 0)) > 5*time.Minute {
		return fmt.Errorf("slack timestamp out of range")
	}
	base := signatureVersion + ":" + ts + ":" + string(rawBody)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	_, _ = mac.Write([]byte(base))
	expected := signatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(sig)) {
		return fmt.Errorf("invalid slack signature")
	}
	return nil
}
