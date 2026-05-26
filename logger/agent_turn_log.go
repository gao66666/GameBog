package logger

import (
	"os"
	"path/filepath"

	"github.com/gao66666/GoBlog/setting"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var agentTurnLg *zap.Logger

func initAgentTurnLogger(cfg *setting.LogConfig) error {
	filename := cfg.AgentTurnFilename
	if filename == "" {
		filename = "./logger/agent_turn_log/agent_turn.log"
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return err
	}

	syncer := getLogWriter(filename, cfg.MaxSize, cfg.MaxBackups, cfg.MaxAge)
	core := zapcore.NewCore(getProdEncoder(), syncer, zapcore.InfoLevel)
	agentTurnLg = zap.New(core)
	return nil
}

// LogAgentTurn 将整轮生命周期写入 logger/agent_turn_log/（与 webServer.log 分离）。
func LogAgentTurn(record any) {
	if agentTurnLg == nil {
		return
	}
	agentTurnLg.Info("agent_turn", zap.Any("turn", record))
}
