package agentstream

import "encoding/json"

// FactEnvelope Agent fact 层 SSE 帧（见 shared/stream_schema.json）。
type FactEnvelope struct {
	V          int             `json:"v"`
	Kind       string          `json:"kind"`
	Seq        int             `json:"seq"`
	RequestID  string          `json:"request_id"`
	Ts         int64           `json:"ts"`
	Phase      string          `json:"phase"`
	Status     string          `json:"status"`
	DurationMs *int            `json:"duration_ms,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}

// LegacyEvent 旧版 Agent SSE（无 v 字段）。
type LegacyEvent struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// IsFactFrame 判断是否为 v1 fact 帧。
func IsFactFrame(raw map[string]json.RawMessage) bool {
	v, ok := raw["v"]
	if !ok {
		return false
	}
	var n int
	if json.Unmarshal(v, &n) != nil || n != 1 {
		return false
	}
	_, ok = raw["kind"]
	return ok
}

// IsLegacyMirror 旧版镜像事件（Agent legacy_mirror 产出）。
func IsLegacyMirror(raw map[string]json.RawMessage) bool {
	if IsFactFrame(raw) {
		return false
	}
	_, ok := raw["type"]
	return ok
}

// ParseFactEnvelope 解析 fact 帧。
func ParseFactEnvelope(data []byte) (FactEnvelope, bool) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || !IsFactFrame(raw) {
		return FactEnvelope{}, false
	}
	var env FactEnvelope
	if json.Unmarshal(data, &env) != nil {
		return FactEnvelope{}, false
	}
	return env, true
}
