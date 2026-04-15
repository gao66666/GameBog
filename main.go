package main

import (
	"fmt"

	"github.com/gao66666/GoBlog/bootstrap"
	"go.uber.org/zap"
)

func main() {
	runtime, err := bootstrap.Init("./setting/common.yaml")
	if err != nil {
		panic("bootstrap init error: " + err.Error())
	}
	defer runtime.Cleanup()

	zap.L().Info("Gin HTTP 服务准备就绪", zap.String("addr", runtime.Addr()))
	fmt.Printf("WebSocket 终端已就绪: ws://localhost%s/ws?token=你的Token\n", runtime.Addr())

	err = runtime.Engine.Run(runtime.Addr())
	if err != nil {
		zap.L().Fatal("Server 启动失败", zap.Error(err))
	}
}
