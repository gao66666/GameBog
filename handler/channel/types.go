package channel

import "net/http"

// 通道名称（与 URL /channel/:name/webhook 一致）。
const (
	NameFeishu = "feishu"
	NameWeCom  = "wecom"
)

// Inbound 各适配器归一化后的入站用户消息。
type Inbound struct {
	Channel      string
	TenantID     string // 应用/企业维度：飞书 app_id、企微 corp_id
	EventID      string // 幂等去重
	ExternalUser string // 平台用户标识：open_id、userid
	ChatKey      string // 会话键：chat_id 等
	Text         string
	MessageKind  string // text | image | ...
	IsGroup      bool
	MentionBot   bool
	Meta         map[string]string // 适配器发消息所需的回传字段
}

// Outbound 统一出站文本（后续可扩展卡片等）。
type Outbound struct {
	Channel string
	TenantID string
	Text     string
	Meta     map[string]string
}

// WebhookInput HTTP 回调原始输入。
type WebhookInput struct {
	RawBody []byte
	Header  http.Header
}

// WebhookResult 适配器解析 webhook 后的同步响应；非空 Async 由 Hub 异步处理。
type WebhookResult struct {
	HTTPStatus int
	Body       any
	Async      *Inbound
}
