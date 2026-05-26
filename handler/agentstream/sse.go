package agentstream

import (
	"encoding/json"
	"fmt"
)

// FormatPresentationSSE 将 presentation 事件格式化为 SSE data 行（含尾随空行）。
func FormatPresentationSSE(ev PresentationEvent) string {
	b, _ := json.Marshal(ev)
	return fmt.Sprintf("data: %s\n\n", b)
}

func formatLegacyJSON(payload []byte) string {
	return fmt.Sprintf("data: %s\n\n", payload)
}

// ProcessAgentDataPayload 处理 Agent 一条 SSE JSON payload。
// sawFact：本流是否已出现过 v1 fact（用于跳过 Agent legacy 镜像）。
func ProcessAgentDataPayload(
	tr *Translator,
	payload []byte,
	sawFact *bool,
) (outLines []string, answerDelta string, streamErr bool) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(payload, &raw) != nil {
		return []string{FormatPresentationSSE(PresentationEvent{
			Type:    "error",
			Content: "流式数据解析失败",
		})}, "", true
	}

	if IsFactFrame(raw) {
		env, ok := ParseFactEnvelope(payload)
		if !ok {
			return nil, "", false
		}
		if tr == nil {
			return []string{formatLegacyJSON(payload)}, "", false
		}
		*sawFact = true
		result := tr.Translate(env)
		if result.StreamError {
			streamErr = true
		}
		answerDelta = result.AnswerDelta
		for _, ev := range result.Events {
			outLines = append(outLines, FormatPresentationSSE(ev))
		}
		return outLines, answerDelta, streamErr
	}

	if *sawFact && IsLegacyMirror(raw) {
		return nil, "", false
	}

	outLines = []string{formatLegacyJSON(payload)}
	var leg LegacyEvent
	if json.Unmarshal(payload, &leg) == nil {
		switch leg.Type {
		case "token":
			answerDelta = leg.Content
		case "error":
			streamErr = true
		}
	}
	return outLines, answerDelta, streamErr
}
