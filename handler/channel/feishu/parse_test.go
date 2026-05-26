package feishu

import (
	"testing"

	ch "github.com/gao66666/GoBlog/handler/channel"
)

func TestParseInboundTextP2P(t *testing.T) {
	raw := []byte(`{
	  "schema": "2.0",
	  "header": {
	    "event_id": "evt_1",
	    "event_type": "im.message.receive_v1",
	    "token": "t"
	  },
	  "event": {
	    "message": {
	      "message_id": "om_1",
	      "chat_id": "oc_1",
	      "chat_type": "p2p",
	      "message_type": "text",
	      "content": "{\"text\":\"你好\"}"
	    },
	    "sender": {
	      "sender_id": { "open_id": "ou_abc" }
	    }
	  }
	}`)
	msg, err := ParseInbound(raw, "t")
	if err != nil || msg == nil {
		t.Fatalf("parse: err=%v msg=%v", err, msg)
	}
	in := toChannelInbound("app_1", msg)
	if in.Text != "你好" || !ch.ShouldProcess(in) {
		t.Fatalf("unexpected inbound: %+v", in)
	}
}

func TestParseInboundGroupWithoutMention(t *testing.T) {
	raw := []byte(`{
	  "schema": "2.0",
	  "header": {
	    "event_id": "evt_2",
	    "event_type": "im.message.receive_v1"
	  },
	  "event": {
	    "message": {
	      "message_id": "om_2",
	      "chat_id": "oc_grp",
	      "chat_type": "group",
	      "message_type": "text",
	      "content": "{\"text\":\"hi\"}"
	    },
	    "sender": {
	      "sender_id": { "open_id": "ou_x" }
	    }
	  }
	}`)
	msg, err := ParseInbound(raw, "")
	if err != nil || msg == nil {
		t.Fatalf("parse: err=%v", err)
	}
	in := toChannelInbound("app_1", msg)
	if ch.ShouldProcess(in) {
		t.Fatal("group without mention should not process")
	}
}

func TestParseChallenge(t *testing.T) {
	raw := []byte(`{"type":"url_verification","token":"sec","challenge":"ch_123"}`)
	challenge, ok := ParseChallenge(raw, Config{VerificationToken: "sec"})
	if !ok || challenge != "ch_123" {
		t.Fatalf("challenge=%q ok=%v", challenge, ok)
	}
}
