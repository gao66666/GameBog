package router

import (
	"net/http"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/handler"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/middleware"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/setting"
	"github.com/gin-gonic/gin"
)

func RouterInit(mode string, redisRepo *database.RedisRepository) *gin.Engine {
	// 1. 设置运行模式
	switch mode {
	case "dev":
		gin.SetMode(gin.DebugMode)
	case "release":
		gin.SetMode(gin.ReleaseMode)
	default:
		gin.SetMode(gin.TestMode)
	}

	//初始化数据库、service、handler
	userRepo, articleRepo := database.MysqlInit(setting.Conf.MySQLConfig)

	user_se := service.NewUserService(userRepo, redisRepo) //将数据库和redis repo实例交给服务层(业务处理)
	user_hd := handler.NewUserHandler(user_se)             //将服务层实例交给handler

	article_se := service.NewArticleService(articleRepo, redisRepo) //将数据库和redis repo实例交给服务层(业务处理)
	article_hd := handler.NewArticleHandler(article_se)             //将服务层实例交给handler

	//初始化Gin引擎
	r := gin.New()
	// 全局中间件：日志和异常恢复
	r.Use(logger.GinLogger(), logger.GinRecovery(true))
	// --- 公开路由 ---
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.POST("/signup", user_hd.SignUpHandle)
	r.POST("/login", user_hd.LoginHandle)
	r.POST("/read/:id", article_hd.ReadArticleHandle)

	// --- 受保护路由组 ---
	// 假设未来你有发布文章、修改资料等接口
	authGroup := r.Group("/v1")
	authGroup.Use(middleware.JWTAuthMiddleware()) // 在这里应用你的 JWT 中间件
	{
		// 示例：只有登录后才能调用的接口
		authGroup.POST("/post/update", user_hd.UpdateUserHandle)
		authGroup.POST("/post/create", article_hd.CreateArticleHandle)
		authGroup.GET("/post/list", article_hd.GetArticleListHandler)
		authGroup.POST("/post/delete/:id", article_hd.DeleteArticleHandle)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"msg": "404 not found"})
	})

	return r
}
