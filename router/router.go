package router

import (
	"net/http"

	"github.com/gao66666/GoBlog/handler"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gin-gonic/gin"
)

func RouterInit(mode string, hd *handler.UserHandler) *gin.Engine {
	switch mode {
	case "dev":
		gin.SetMode(gin.DebugMode)
	case "release":
		gin.SetMode(gin.ReleaseMode)
	default:
		gin.SetMode(gin.TestMode)
	}
	r := gin.New()
	r.Use(logger.GinLogger(), logger.GinRecovery(true))
	r.POST("/signup", hd.SignUpHandle)
	r.POST("/login", hd.LoginHandle)
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"msg": "404"})
	})
	return r
}
