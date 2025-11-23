package logger

import (
	"path/filepath"
	"time"

	"github.com/gao66666/GoBlog/setting"

	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 在 logger 包内部定义级别配置
type internalConfig struct {
	mainLogLevel  zapcore.Level
	errorLogLevel zapcore.Level
}

var (
	// 开发环境内部配置
	devConfig = internalConfig{
		mainLogLevel:  zapcore.DebugLevel,
		errorLogLevel: zapcore.DebugLevel, // 开发环境错误日志也记录所有
	}
	// 生产环境内部配置
	prodConfig = internalConfig{
		mainLogLevel:  zapcore.InfoLevel,  // 生产环境主日志从Info开始
		errorLogLevel: zapcore.ErrorLevel, // 错误日志只记录Error及以上
	}
)

func getProductionCore(cfg *setting.LogConfig) zapcore.Core {
	mainSyncer := getLogWriter(cfg.Common_filename, cfg.MaxSize, cfg.MaxBackups, cfg.MaxAge)
	errorFilename := getErrorFilename(cfg.Error_filename)
	errorSyncer := getLogWriter(errorFilename, cfg.MaxSize, cfg.MaxBackups, cfg.MaxAge)

	return zapcore.NewTee(
		zapcore.NewCore(getProdEncoder(), mainSyncer, prodConfig.mainLogLevel),
		zapcore.NewCore(getProdEncoder(), errorSyncer, prodConfig.errorLogLevel),
	)
}

// getErrorFilename 生成错误日志文件名
func getErrorFilename(baseFilename string) string {
	ext := filepath.Ext(baseFilename)
	name := baseFilename[:len(baseFilename)-len(ext)]
	return name + "_error" + ext
}

// getDevEncoder 开发环境编码器
func getDevEncoder() zapcore.Encoder {
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	encoderConfig.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
		enc.AppendString(t.Format("15:04:05.000"))
	}
	encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	encoderConfig.EncodeDuration = zapcore.StringDurationEncoder
	encoderConfig.ConsoleSeparator = " | "
	return zapcore.NewConsoleEncoder(encoderConfig)
}

// getProdEncoder 生产环境编码器
func getProdEncoder() zapcore.Encoder {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	encoderConfig.LevelKey = "level"
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	encoderConfig.CallerKey = "caller"
	encoderConfig.EncodeDuration = zapcore.SecondsDurationEncoder
	encoderConfig.MessageKey = "message"
	return zapcore.NewJSONEncoder(encoderConfig)
}

// getLogWriter 日志文件写入器
func getLogWriter(filename string, maxSize, maxBackup, maxAge int) zapcore.WriteSyncer {
	lumberJackLogger := &lumberjack.Logger{
		Filename:   filename,
		MaxSize:    maxSize,
		MaxBackups: maxBackup,
		MaxAge:     maxAge,
	}
	return zapcore.AddSync(lumberJackLogger)
}
