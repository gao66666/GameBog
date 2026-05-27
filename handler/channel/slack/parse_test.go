package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestVerifyRequest(t *testing.T) {
	secret := "test_secret"
	body := []byte(`{"type":"url_verification","challenge":"abc"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	base := signatureVersion + ":" + ts + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(base))
	sig := signatureVersion + "=" + hex.EncodeToString(mac.Sum(nil))
	h := http.Header{}
	h.Set("X-Slack-Request-Timestamp", ts)
	h.Set("X-Slack-Signature", sig)
	if err := VerifyRequest(h, secret, body); err != nil {
		t.Fatal(err)
	}
}

func TestParseInboundMessage(t *testing.T) {
	raw := []byte(`{
	  "type":"event_callback",
	  "event_id":"Ev1",
	  "event":{
	    "type":"message",
	    "user":"U1",
	    "text":"hello",
	    "channel":"C1",
	    "channel_type":"im"
	  }
	}`)
	msg, challenge, err := ParseInbound(raw, "")
	if err != nil || challenge != "" {
		t.Fatalf("challenge=%q err=%v", challenge, err)
	}
	if msg == nil || msg.Text != "hello" || msg.UserID != "U1" {
		t.Fatalf("msg=%+v", msg)
	}
}

func TestParseInboundChallenge(t *testing.T) {
	raw := []byte(`{"type":"url_verification","challenge":"xyz"}`)
	msg, challenge, err := ParseInbound(raw, "")
	if err != nil || msg != nil || challenge != "xyz" {
		t.Fatalf("msg=%v challenge=%q err=%v", msg, challenge, err)
	}
}
