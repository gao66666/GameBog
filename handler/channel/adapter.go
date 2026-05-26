package channel

import "context"

// Adapter 将具体 IM 平台与统一收发层对接：解析回调、发送回复。
type Adapter interface {
	// Name 通道标识，如 feishu、wecom。
	Name() string
	// Enabled 是否已配置并可接收流量。
	Enabled() bool
	// HandleWebhook 验签/解密、URL 校验、解析为统一 Inbound；仅同步返回平台要求的 ACK。
	HandleWebhook(ctx context.Context, in WebhookInput) (WebhookResult, error)
	// Send 将助手回复发回该平台。
	Send(ctx context.Context, out Outbound) error
}
