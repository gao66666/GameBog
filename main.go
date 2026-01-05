package main

import (
	"fmt"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/router"
	"github.com/gao66666/GoBlog/setting"
	"github.com/gao66666/GoBlog/tool"
	"go.uber.org/zap"
)

func main() {
	//1 初始化配置
	if err := setting.Init("./setting/common.yaml"); err != nil {
		panic("Config load error")
	}
	setting.Conf.Mode = "dev"

	//2 初始化日志
	if err := logger.Init(setting.Conf.LogConfig, setting.Conf.Mode); err != nil {
		panic("Logger init error")
	}
	defer zap.L().Sync()

	//初始化雪花算法节点
	if err := tool.InitSnowflake(setting.Conf.MachineID); err != nil {
		panic("雪花算法初始化失败: " + err.Error())
	}

	//初始化 Redis
	redisRepo, err := database.RedisInit(setting.Conf.RedisConfig)
	if err != nil {
		panic("Redis init error")
	}
	defer redisRepo.Close()
	r := router.RouterInit(setting.Conf.Mode, redisRepo)

	// 4. 启动服务
	err = r.Run(fmt.Sprintf(":%d", setting.Conf.Port))
	if err != nil {
		panic("Server start error: " + err.Error())
	}
	zap.L().Info("Init has been finished")
}
