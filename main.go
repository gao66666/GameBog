package main

import (
	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/setting"
	"go.uber.org/zap"
)

func main() {
	//1 初始化配置
	if err := setting.Init("./setting/common.yaml"); err != nil {
		panic("setting init error")
	}
	setting.Conf.Mode = "dev"

	//2 初始化日志
	if err := logger.Init(setting.Conf.LogConfig, setting.Conf.Mode); err != nil {
		panic("Logger init error")
	}
	defer zap.L().Sync()

	//3 初始化 Redis
	if err := database.RedisInit(setting.Conf.RedisConfig); err != nil {
		zap.L().Error("Redis init failed", zap.Error(err))
		panic("Redis init error")
	}
	defer database.RedisClose()

	//4 初始化数据库

	zap.L().Info("Init has been finished")
}
