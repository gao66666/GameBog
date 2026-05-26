package agentstream

import (
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/mq"
	"go.uber.org/zap"
)

// PublishAgentTurn 写入 logger/agent_turn_log/；Kafka 可用时异步投递（失败不影响主链路）。
func PublishAgentTurn(record AgentTurnRecord) {
	logger.LogAgentTurn(record)
	if !mq.AgentTurnKafkaAvailable() {
		return
	}
	rec := record
	go func() {
		if err := mq.PublishAgentTurn(&rec); err != nil {
			zap.L().Warn("agent_turn kafka publish failed",
				zap.String("request_id", rec.RequestID),
				zap.Error(err),
			)
		}
	}()
}
