package channel

import "strings"

// ShouldProcess 是否应对入站消息调用 Agent。
func ShouldProcess(in *Inbound) bool {
	if in == nil {
		return false
	}
	if in.IsGroup && !in.MentionBot {
		return false
	}
	if in.MessageKind != "text" {
		return false
	}
	return strings.TrimSpace(in.Text) != ""
}

// UnsupportedHint 非文本等场景的提示文案。
func UnsupportedHint(in *Inbound) string {
	if in == nil {
		return ""
	}
	if in.MessageKind != "text" {
		return "暂仅支持文字消息，请直接发送文本。"
	}
	return ""
}
