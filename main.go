package main

import (
	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/handler"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/router"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/setting"
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
	if err := database.InitSnowflake(setting.Conf.MachineID); err != nil {
		panic("雪花算法初始化失败: " + err.Error())
	}

	//3 初始化 Redis
	redisRepo, err := database.RedisInit(setting.Conf.RedisConfig)
	if err != nil {
		panic("Redis init error")
	}
	defer redisRepo.Close()

	//4 初始化数据库
	sqlRepo := database.MysqlInit(setting.Conf.MySQLConfig)
	// 初始化数据库并获取Repository
	//5
	user_se := service.NewUserService(sqlRepo, redisRepo) //将数据库和redis repo实例交给服务层(业务处理)
	hd := handler.NewUserHandler(user_se)                 //将服务层实例交给handler
	// 6 注册路由
	router.RouterInit(setting.Conf.Mode, hd) //将handler repo交给 router进行注册
	zap.L().Info("Init has been finished")
}
