package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/handler/agentstream"
	"go.uber.org/zap"
)

// AgentChatRunInput 调用 Python Agent /chat 的入参（网关侧已解析会话与历史）。
type AgentChatRunInput struct {
	UserID        uint64
	Message       string
	Token         string
	ChatSessionID string
	RequestID     string
}

// AgentChatRunResult 消费 SSE 后的结果。
type AgentChatRunResult struct {
	Assistant     string
	ChatSessionID string
	StreamErr     bool
	Aborted       bool
	TurnRecord    agentstream.AgentTurnRecord
}

// AgentChatStreamSink 可选：将翻译后的事件写给浏览器等；为 nil 时仅累积最终回复文本。
type AgentChatStreamSink func(outLine string) error

// RunAgentChat 请求 Agent SSE、拼 assistant，并在成功时写入网关短期对话 Redis。
func (h *AgentHandler) RunAgentChat(ctx context.Context, in AgentChatRunInput, sink AgentChatStreamSink) (AgentChatRunResult, error) {
	var empty AgentChatRunResult
	if h == nil || h.agentURL == "" {
		return empty, fmt.Errorf("agent not configured")
	}
	msg := strings.TrimSpace(in.Message)
	if msg == "" {
		return empty, fmt.Errorf("empty message")
	}

	start := time.Now()
	chatSessionID := strings.TrimSpace(in.ChatSessionID)

	ar := agentRequest{
		Message:               msg,
		UserID:                in.UserID,
		Token:                 in.Token,
		ChatSessionID:         chatSessionID,
		RequestID:             in.RequestID,
		HistoryOwnedByGateway: h.chatStore != nil,
	}
	if h.chatStore != nil && in.UserID > 0 && chatSessionID != "" {
		prev, err := h.chatStore.ListMessages(ctx, in.UserID, chatSessionID, agentChatMaxShortMessages)
		if err != nil {
			zap.L().Warn("读取网关对话缓存失败", zap.Error(err))
		} else if len(prev) > 0 {
			ar.ConversationHistory = make([]map[string]string, 0, len(prev))
			for _, m := range prev {
				ar.ConversationHistory = append(ar.ConversationHistory, map[string]string{
					"role":    m.Role,
					"content": m.Content,
				})
			}
		}
	}

	body, err := json.Marshal(ar)
	if err != nil {
		return empty, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.agentURL+"/chat", bytes.NewReader(body))
	if err != nil {
		return empty, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if in.RequestID != "" {
		httpReq.Header.Set("X-Request-Id", in.RequestID)
	}

	resp, err := h.httpClient.Do(httpReq)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("agent http %d", resp.StatusCode)
	}

	var tokenBuf strings.Builder
	streamErr := false
	sawFact := false
	turnAgg := agentstream.NewTurnAggregator(agentstream.TurnMeta{
		RequestID:     in.RequestID,
		UserID:        in.UserID,
		ChatSessionID: chatSessionID,
		UserMessage:   msg,
		StartedAt:     start,
	})

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}
		line := scanner.Text()
		if line == "" {
			if sink != nil {
				if err := sink("\n"); err != nil {
					break
				}
			}
			continue
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "data: ") {
			payload := []byte(trim[6:])
			if env, ok := agentstream.ParseFactEnvelope(payload); ok {
				turnAgg.Observe(env)
			}
			outLines, delta, errFlag := agentstream.ProcessAgentDataPayload(
				h.streamTranslator,
				payload,
				&sawFact,
			)
			if delta != "" {
				tokenBuf.WriteString(delta)
			}
			if errFlag {
				streamErr = true
			}
			if sink != nil {
				for _, out := range outLines {
					if err := sink(out); err != nil {
						goto done
					}
				}
			}
			continue
		}
		if sink != nil {
			if err := sink(line + "\n"); err != nil {
				break
			}
		}
	}
done:
	if err := scanner.Err(); err != nil {
		zap.L().Warn("读取 Agent SSE 流出错", zap.Error(err))
		streamErr = true
	}

	assistant := strings.TrimSpace(tokenBuf.String())
	turnRecord := turnAgg.Finalize(agentstream.TurnFinalizeInput{
		OutputText: assistant,
		StreamErr:  streamErr,
		Aborted:    ctx.Err() != nil,
	})
	agentstream.PublishAgentTurn(turnRecord)

	if !streamErr && ctx.Err() == nil && h.chatStore != nil && in.UserID > 0 && chatSessionID != "" && assistant != "" {
		ttl := 60 * time.Minute
		_ = h.chatStore.AppendMessages(ctx, in.UserID, chatSessionID, []database.AgentChatMessage{
			{Role: "user", Content: msg},
			{Role: "assistant", Content: assistant},
		}, ttl, agentChatMaxShortMessages)
	}

	return AgentChatRunResult{
		Assistant:     assistant,
		ChatSessionID: chatSessionID,
		StreamErr:     streamErr,
		Aborted:       ctx.Err() != nil,
		TurnRecord:    turnRecord,
	}, nil
}

// AgentConfigured 是否已配置 AGENT_URL。
func (h *AgentHandler) AgentConfigured() bool {
	return h != nil && h.agentURL != ""
}

// TryAcquireAgentSlot 占用 Agent 并发槽；IM 与 Web 共用 sem。
func (h *AgentHandler) TryAcquireAgentSlot() bool {
	if h == nil {
		return false
	}
	select {
	case h.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (h *AgentHandler) ReleaseAgentSlot() {
	if h == nil {
		return
	}
	select {
	case <-h.sem:
	default:
	}
}
