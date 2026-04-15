package tool

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResponseSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("request_id", "req-1")

	ResponseSuccess(c, gin.H{"k": "v"}, "ok")

	if w.Code != 200 {
		t.Fatalf("unexpected status: %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body["code"].(float64) != 0 {
		t.Fatalf("unexpected code: %v", body["code"])
	}
	if body["request_id"].(string) != "req-1" {
		t.Fatalf("request_id not found")
	}
}

func TestResponseErrorBiz(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	ResponseError(c, NewBizError(401, 40002, "invalid token"))

	if w.Code != 401 {
		t.Fatalf("unexpected status: %d", w.Code)
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body["code"].(float64) != 40002 {
		t.Fatalf("unexpected code: %v", body["code"])
	}
}
