package dingtalk

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestVerifyRequest(t *testing.T) {
	secret := "SEC123"
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	stringToSign := ts + "\n" + secret
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	h := http.Header{}
	h.Set("timestamp", ts)
	h.Set("sign", sign)
	if err := VerifyRequest(h, secret, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
}

func TestParseInboundText(t *testing.T) {
	raw := []byte(`{
	  "msgtype":"text",
	  "msgId":"m1",
	  "conversationId":"c1",
	  "conversationType":"1",
	  "senderId":"u1",
	  "sessionWebhook":"https://example.com/hook",
	  "text":{"content":"你好"}
	}`)
	msg, err := ParseInbound(raw)
	if err != nil || msg == nil {
		t.Fatalf("err=%v msg=%v", err, msg)
	}
	if msg.Text != "你好" || msg.SessionWebhook == "" {
		t.Fatalf("msg=%+v", msg)
	}
}
